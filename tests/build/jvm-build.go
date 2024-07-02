package build

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/devfile/library/v2/pkg/util"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/jvm-build-service/openshift-with-appstudio-test/e2e"
	"github.com/redhat-appstudio/jvm-build-service/pkg/apis/jvmbuildservice/v1alpha1"
	jvmclientSet "github.com/redhat-appstudio/jvm-build-service/pkg/client/clientset/versioned"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	pipelineclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	testProjectGitUrl   = utils.GetEnv("JVM_BUILD_SERVICE_TEST_REPO_URL", "https://github.com/redhat-appstudio-qe/hacbs-test-project")
	testProjectRevision = utils.GetEnv("JVM_BUILD_SERVICE_TEST_REPO_REVISION", "34da5a8f51fba6a8b7ec75a727d3c72ebb5e1274")
)

var _ = framework.JVMBuildSuiteDescribe("JVM Build Service E2E tests", Label("jvm-build", "HACBS"), func() {
	var f *framework.Framework
	AfterEach(framework.ReportFailure(&f))
	var err error

	defer GinkgoRecover()

	var testNamespace, applicationName, componentName string
	var component *appservice.Component
	var timeout, interval time.Duration
	var customJavaBuilderPipelineAnnotation map[string]string

	AfterAll(func() {
		jvmClient := jvmclientSet.New(f.AsKubeAdmin.JvmbuildserviceController.JvmbuildserviceClient().JvmbuildserviceV1alpha1().RESTClient())
		tektonClient := pipelineclientset.New(f.AsKubeAdmin.TektonController.PipelineClient().TektonV1().RESTClient())
		kubeClient := kubernetes.New(f.AsKubeAdmin.CommonController.KubeInterface().CoreV1().RESTClient())
		//status report ends up in artifacts/redhat-appstudio-e2e/redhat-appstudio-e2e/artifacts/rp_preproc/attachments/xunit
		e2e.GenerateStatusReport(testNamespace, jvmClient, kubeClient, tektonClient)
		if !CurrentSpecReport().Failed() {
			Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
			Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
			Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
			Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			Expect(f.AsKubeAdmin.JvmbuildserviceController.DeleteJBSConfig(constants.JBSConfigName, testNamespace)).To(Succeed())
		} else {
			Expect(f.AsKubeAdmin.CommonController.StoreAllPods(testNamespace)).To(Succeed())
			Expect(f.AsKubeAdmin.TektonController.StoreAllPipelineRuns(testNamespace)).To(Succeed())
		}
	})

	BeforeAll(func() {
		f, err = framework.NewFramework(utils.GetGeneratedNamespace("jvm-build"))
		Expect(err).NotTo(HaveOccurred())
		testNamespace = f.UserNamespace
		Expect(testNamespace).NotTo(BeNil(), "failed to create sandbox user namespace")

		_, err = f.AsKubeAdmin.JvmbuildserviceController.CreateJBSConfig(constants.JBSConfigName, testNamespace)
		Expect(err).ShouldNot(HaveOccurred())

		//wait for the cache

		Expect(f.AsKubeAdmin.JvmbuildserviceController.WaitForCache(f.AsKubeAdmin.CommonController, testNamespace)).Should(Succeed())

		customJavaPipelineBundleRef := os.Getenv(constants.CUSTOM_JAVA_PIPELINE_BUILD_BUNDLE_ENV)
		if len(customJavaPipelineBundleRef) > 0 {
			customJavaBuilderPipelineAnnotation = map[string]string{
				"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"java-builder", "bundle": "%s"}`, customJavaPipelineBundleRef),
			}
		}

		timeout = time.Minute * 20
		interval = time.Second * 10

		applicationName = fmt.Sprintf("jvm-build-suite-application-%s", util.GenerateRandomString(4))
		_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		componentName = fmt.Sprintf("jvm-build-suite-component-%s", util.GenerateRandomString(6))

		// Create a component with Git Source URL being defined
		componentObj := appservice.ComponentSpec{
			ComponentName: componentName,
			Source: appservice.ComponentSource{
				ComponentSourceUnion: appservice.ComponentSourceUnion{
					GitSource: &appservice.GitSource{
						URL:      testProjectGitUrl,
						Revision: testProjectRevision,
					},
				},
			},
		}
		component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, true, customJavaBuilderPipelineAnnotation)
		Expect(err).ShouldNot(HaveOccurred())
	})

	When("the Component with s2i-java component is created", func() {
		It("a PipelineRun is triggered", func() {
			Eventually(func() error {
				pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				if err != nil {
					GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s", testNamespace, componentName)
					return err
				}
				if !pr.HasStarted() {
					return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
				}
				return nil
			}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
		})

		It("the build-container task from component pipelinerun references a correct analyzer image", func() {
			ciAnalyzerImage := os.Getenv("JVM_BUILD_SERVICE_REQPROCESSOR_IMAGE")
			matchingTaskStep := "analyse-dependencies-java-sbom"

			if ciAnalyzerImage == "" {
				Skip("JVM_BUILD_SERVICE_REQPROCESSOR_IMAGE env var is not exported, skipping the test...")
			}

			Eventually(func() error {
				pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())

				for _, chr := range pr.Status.ChildReferences {
					taskRun := &pipeline.TaskRun{}
					taskRunKey := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
					err := f.AsKubeAdmin.CommonController.KubeRest().Get(context.Background(), taskRunKey, taskRun)
					Expect(err).ShouldNot(HaveOccurred())

					prTrStatus := &pipeline.PipelineRunTaskRunStatus{
						PipelineTaskName: chr.PipelineTaskName,
						Status:           &taskRun.Status,
					}

					if chr.PipelineTaskName == constants.BuildTaskRunName && prTrStatus.Status != nil && prTrStatus.Status.TaskSpec != nil && prTrStatus.Status.TaskSpec.Steps != nil {
						for _, step := range prTrStatus.Status.TaskSpec.Steps {
							if step.Name == matchingTaskStep {
								if step.Image != ciAnalyzerImage {
									Fail(fmt.Sprintf("the build-container task from component pipelinerun doesn't reference the correct request processor image. expected: %v, actual: %v", ciAnalyzerImage, step.Image))
								} else {
									return nil
								}
							}
						}
					}
				}
				return fmt.Errorf("couldn't find a matching step %s in task %s in PipelineRun %s/%s", matchingTaskStep, constants.BuildTaskRunName, testNamespace, pr.GetName())
			}, timeout, interval).Should(Succeed(), "timed out when verifying the request processor image reference in pipelinerun")
		})

		It("that PipelineRun completes successfully", func() {
			Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "",
				f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())

			pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
			Expect(err).ShouldNot(HaveOccurred())
			//now delete it so it can't interfere with later test logic
			Expect(f.AsKubeAdmin.TektonController.DeletePipelineRun(pr.Name, testNamespace)).Should(Succeed())
		})

		It("artifactbuilds and dependencybuilds are generated", func() {
			Eventually(func() bool {
				var gotABs, gotDBs bool
				abList, err := f.AsKubeAdmin.JvmbuildserviceController.ListArtifactBuilds(testNamespace)
				if err != nil {
					GinkgoWriter.Printf("error listing artifactbuilds: %s\n", err.Error())
					return false
				}
				if len(abList.Items) > 0 {
					gotABs = true
				}
				dbList, err := f.AsKubeAdmin.JvmbuildserviceController.ListDependencyBuilds(testNamespace)
				if err != nil {
					GinkgoWriter.Printf("error listing dependencybuilds: %s\n", err.Error())
					return false
				}
				if len(dbList.Items) > 0 {
					gotDBs = true
				}
				GinkgoWriter.Printf("got artifactbuilds: %t, got dependencybuilds: %t\n", gotABs, gotDBs)
				if !gotABs || !gotDBs {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue(), "timed out when waiting for the generation of artifactbuilds and dependencybuilds")
		})

		It("some artifactbuilds and dependencybuilds complete", func() {
			Eventually(func() bool {
				var abComplete, dbComplete bool
				abList, err := f.AsKubeAdmin.JvmbuildserviceController.ListArtifactBuilds(testNamespace)
				if err != nil {
					GinkgoWriter.Printf("error listing artifactbuilds: %s\n", err.Error())
					return false
				}
				for _, ab := range abList.Items {
					if ab.Status.State == v1alpha1.ArtifactBuildStateComplete {
						abComplete = true
						break
					}
				}
				dbList, err := f.AsKubeAdmin.JvmbuildserviceController.ListDependencyBuilds(testNamespace)
				if err != nil {
					GinkgoWriter.Printf("error listing dependencybuilds: %s\n", err.Error())
					return false
				}
				for _, db := range dbList.Items {
					if db.Status.State == v1alpha1.DependencyBuildStateComplete {
						dbComplete = true
						break
					}
				}
				GinkgoWriter.Printf("some artifactbuilds completed: %t, some dependencybuilds completed: %t\n", abComplete, dbComplete)
				if !abComplete || !dbComplete {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for some artifactbuilds and dependencybuilds to complete in %s namespace", testNamespace))
		})

		It("all artifactbuild and dependencybuilds complete", func() {
			Eventually(func() bool {
				abList, err := f.AsKubeAdmin.JvmbuildserviceController.ListArtifactBuilds(testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), "error in listing artifact builds")
				// we want to make sure there is more than one ab and that they are all complete
				allAbCompleted := len(abList.Items) > 0
				GinkgoWriter.Printf("number of artifactbuilds: %d\n", len(abList.Items))
				for _, ab := range abList.Items {
					if ab.Status.State != v1alpha1.ArtifactBuildStateComplete {
						GinkgoWriter.Printf("artifactbuild %s not complete\n", ab.Spec.GAV)
						allAbCompleted = false
						break
					}
				}
				dbList, err := f.AsKubeAdmin.JvmbuildserviceController.ListDependencyBuilds(testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), "error in listing dependency builds")
				allDbCompleted := len(dbList.Items) > 0
				GinkgoWriter.Printf("number of dependencybuilds: %d\n", len(dbList.Items))
				for _, db := range dbList.Items {
					if db.Status.State != v1alpha1.DependencyBuildStateComplete {
						GinkgoWriter.Printf("dependencybuild %s not complete\n", db.Spec.ScmInfo.SCMURL)
						allDbCompleted = false
						break
					} else if db.Status.State == v1alpha1.DependencyBuildStateFailed {
						Fail(fmt.Sprintf("dependencybuild %s FAILED", db.Spec.ScmInfo.SCMURL))
					}
				}
				if allAbCompleted && allDbCompleted {
					return true
				}
				return false
			}, 2*timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for all artifactbuilds and dependencybuilds to complete in %s namespace", testNamespace))
		})

		It("does rebuild using cached dependencies", func() {
			prun := &pipeline.PipelineRun{}

			component, err := f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
			Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("could not get component %s/%s", testNamespace, componentName))

			annotations := utils.MergeMaps(component.GetAnnotations(), constants.ComponentTriggerSimpleBuildAnnotation)
			component.SetAnnotations(annotations)
			Expect(f.AsKubeAdmin.CommonController.KubeRest().Update(context.Background(), component, &client.UpdateOptions{})).To(Succeed())

			Eventually(func() error {
				prun, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				return err
			}, 1*time.Minute, constants.PipelineRunPollingInterval).Should(BeNil(), fmt.Sprintf("timed out when getting the pipelinerun for %s/%s component", testNamespace, componentName))

			ctx := context.Background()

			watch, err := f.AsKubeAdmin.TektonController.GetPipelineRunWatch(ctx, testNamespace)
			Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("watch pipelinerun failed in %s namespace", testNamespace))

			exitForLoop := false

			for {
				select {
				case <-time.After(15 * time.Minute):
					Fail(fmt.Sprintf("timed out waiting for second build to complete in %s namespace", testNamespace))
				case event := <-watch.ResultChan():
					if event.Object == nil {
						continue
					}
					pr, ok := event.Object.(*pipeline.PipelineRun)
					if !ok {
						continue
					}
					if prun.Name != pr.Name {
						if pr.IsDone() {
							GinkgoWriter.Printf("got event for pipelinerun %s in a terminal state\n", pr.GetName())
							continue
						}
						Fail(fmt.Sprintf("another non-completed pipeline run %s/%s was generated when it should not", pr.GetNamespace(), pr.GetName()))
					}
					GinkgoWriter.Printf("done processing event for pr %s\n", pr.GetName())
					if pr.IsDone() {
						GinkgoWriter.Println("pr is done")

						podClient := f.AsKubeAdmin.CommonController.KubeInterface().CoreV1().Pods(testNamespace)
						listOptions := metav1.ListOptions{
							LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", pr.GetName()),
						}
						podList, err := podClient.List(context.Background(), listOptions)
						Expect(err).ShouldNot(HaveOccurred(), "error listing pr pods")

						pods := podList.Items

						if len(pods) == 0 {
							Fail(fmt.Sprintf("pod for pipeline run %s/%s unexpectedly missing", pr.GetNamespace(), pr.GetName()))
						}

						containers := []corev1.Container{}
						containers = append(containers, pods[0].Spec.InitContainers...)
						containers = append(containers, pods[0].Spec.Containers...)

						for _, container := range containers {
							if !strings.Contains(container.Name, "analyse-dependecies") {
								continue
							}
							cLog, err := utils.GetContainerLogs(f.AsKubeAdmin.CommonController.KubeInterface(), pods[0].Name, container.Name, testNamespace)
							Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("getting container logs for %s container from %s pod in %s namespace failed", container.Name, pods[0].GetName(), testNamespace))
							if strings.Contains(cLog, "\"publisher\" : \"central\"") {
								Fail(fmt.Sprintf("pipelinerun %s has container %s with dep analysis still pointing to central %s", pr.Name, container.Name, cLog))
							}
							if !strings.Contains(cLog, "\"publisher\" : \"rebuilt\"") {
								Fail(fmt.Sprintf("pipelinerun %s has container %s with dep analysis that does not access rebuilt %s", pr.Name, container.Name, cLog))
							}
							if !strings.Contains(cLog, "\"java:scm-uri\" : \"https://github.com/stuartwdouglas/hacbs-test-simple-jdk8.git\"") {
								Fail(fmt.Sprintf("pipelinerun %s has container %s with dep analysis did not include java:scm-uri %s", pr.Name, container.Name, cLog))
							}
							if !strings.Contains(cLog, "\"java:scm-commit\" : \"") {
								Fail(fmt.Sprintf("pipelinerun %s has container %s with dep analysis did not include java:scm-commit %s", pr.Name, container.Name, cLog))
							}
							break
						}
						GinkgoWriter.Println("pr is done and has correct analyse-dependecies output, exiting")
						exitForLoop = true
					}
				}
				if exitForLoop {
					break
				}
			}
		})

		It("All rebuilt images are signed and attested", func() {
			seen := map[string]bool{}
			rebuilt, err := f.AsKubeAdmin.JvmbuildserviceController.ListRebuiltArtifacts(testNamespace)
			Expect(err).NotTo(HaveOccurred())
			for _, i := range rebuilt.Items {
				if seen[i.Spec.Image] {
					continue
				}
				seen[i.Spec.Image] = true

				imageWithDigest := i.Spec.Image + "@" + i.Spec.Digest

				Expect(f.AsKubeAdmin.TektonController.AwaitAttestationAndSignature(imageWithDigest, constants.ChainsAttestationTimeout)).To(
					Succeed(),
					"Could not find .att or .sig ImageStreamTags within the 1 minute timeout. "+
						"Most likely the chains-controller did not create those in time. "+
						"Look at the chains-controller logs.")
				GinkgoWriter.Printf("Cosign verify pass with .att and .sig ImageStreamTags found for %s\n", imageWithDigest)

			}
		})
	})
})
