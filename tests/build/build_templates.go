package build

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"

	"github.com/konflux-ci/application-api/api/v1alpha1"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	kubeapi "github.com/konflux-ci/e2e-tests/pkg/clients/kubernetes"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	"github.com/konflux-ci/e2e-tests/pkg/utils/build"
	"github.com/konflux-ci/e2e-tests/pkg/utils/contract"
	"github.com/konflux-ci/e2e-tests/pkg/utils/pipeline"
	"github.com/konflux-ci/e2e-tests/pkg/utils/tekton"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/image/reference"

	tektonpipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	remoteimg "github.com/google/go-containerregistry/pkg/v1/remote"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

var (
	ecPipelineRunTimeout = time.Duration(10 * time.Minute)
)

const pipelineCompletionRetries = 2

// CreateComponent creates a component from a test repository URL and returns the component's name
func CreateComponent(ctrl *has.HasController, gitUrl, revision, applicationName, componentName, namespace string) string {
	var err error
	var buildPipelineAnnotation map[string]string
	contextDir, dockerfilePath, pipelineBundleName, enableHermetic, prefetchInput, checkAdditionalTags := GetComponentScenarioDetailsFromGitUrl(gitUrl)
	Expect(pipelineBundleName).ShouldNot(BeEmpty())
	if pipelineBundleName == "docker-build" {
		customDockerBuildBundle := os.Getenv(constants.CUSTOM_DOCKER_BUILD_PIPELINE_BUNDLE_ENV)
		if enableHermetic {
			//Update the docker-build pipeline bundle with param hermetic=true
			customDockerBuildBundle, err = enableHermeticBuildInPipelineBundle(customDockerBuildBundle, prefetchInput)
			if err != nil {
				GinkgoWriter.Printf("failed to enable hermetic build in the pipeline bundle with: %v\n", err)
				return ""
			}
		}
		if checkAdditionalTags {
			//Update the pipeline bundle to apply additional tags
			customDockerBuildBundle, err = applyAdditionalTagsInPipelineBundle(customDockerBuildBundle, additionalTags)
			if err != nil {
				GinkgoWriter.Printf("failed to apply additinal tags in the pipeline bundle with: %v\n", err)
				return ""
			}
		}
		if customDockerBuildBundle == "" {
			customDockerBuildBundle = "latest"
		}
		buildPipelineAnnotation = map[string]string{
			"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"docker-build", "bundle": "%s"}`, customDockerBuildBundle),
		}
	} else if pipelineBundleName == "fbc-builder" {
		customFbcBuilderBundle := os.Getenv(constants.CUSTOM_FBC_BUILDER_PIPELINE_BUNDLE_ENV)
		if customFbcBuilderBundle == "" {
			customFbcBuilderBundle = "latest"
		}
		buildPipelineAnnotation = map[string]string{
			"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"fbc-builder", "bundle": "%s"}`, customFbcBuilderBundle),
		}
	}

	componentObj := appservice.ComponentSpec{
		ComponentName: componentName,
		Source: appservice.ComponentSource{
			ComponentSourceUnion: appservice.ComponentSourceUnion{
				GitSource: &appservice.GitSource{
					URL:           gitUrl,
					Revision:      revision,
					Context:       contextDir,
					DockerfileURL: dockerfilePath,
				},
			},
		},
	}

	if os.Getenv(constants.CUSTOM_SOURCE_BUILD_PIPELINE_BUNDLE_ENV) != "" {
		customSourceBuildBundle := os.Getenv(constants.CUSTOM_SOURCE_BUILD_PIPELINE_BUNDLE_ENV)
		Expect(customSourceBuildBundle).ShouldNot(BeEmpty())
		buildPipelineAnnotation = map[string]string{
			"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"docker-build", "bundle": "%s"}`, customSourceBuildBundle),
		}
	}
	c, err := ctrl.CreateComponent(componentObj, namespace, "", "", applicationName, false, buildPipelineAnnotation)
	Expect(err).ShouldNot(HaveOccurred())
	return c.Name
}

func WaitForPipelineRunStarts(hub *framework.ControllerHub, applicationName, componentName, namespace string, timeout time.Duration) string {
	namespacedName := fmt.Sprintf("%s/%s", namespace, componentName)
	timeoutMsg := fmt.Sprintf(
		"timed out when waiting for the PipelineRun to start for the Component %s", namespacedName)
	var prName string
	Eventually(func() error {
		pipelineRun, err := hub.HasController.GetComponentPipelineRun(componentName, applicationName, namespace, "")
		if err != nil {
			GinkgoWriter.Printf("PipelineRun has not been created yet for Component %s\n", namespacedName)
			return err
		}
		if !pipelineRun.HasStarted() {
			return fmt.Errorf("pipelinerun %s/%s has not started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
		}
		err = hub.TektonController.AddFinalizerToPipelineRun(pipelineRun, constants.E2ETestFinalizerName)
		if err != nil {
			return fmt.Errorf("error while adding finalizer %q to the pipelineRun %q: %v",
				constants.E2ETestFinalizerName, pipelineRun.GetName(), err)
		}
		prName = pipelineRun.GetName()
		return nil
	}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), timeoutMsg)
	return prName
}

var _ = framework.BuildSuiteDescribe("Build templates E2E test", Label("build", "build-templates", "HACBS"), func() {
	var f *framework.Framework
	var err error
	AfterEach(framework.ReportFailure(&f))

	defer GinkgoRecover()
	Describe("HACBS pipelines", Ordered, Label("pipeline"), func() {

		var applicationName, componentName, symlinkComponentName, testNamespace string
		var kubeadminClient *framework.ControllerHub
		var pipelineRunsWithE2eFinalizer []string

		BeforeAll(func() {
			if os.Getenv("APP_SUFFIX") != "" {
				applicationName = fmt.Sprintf("test-app-%s", os.Getenv("APP_SUFFIX"))
			} else {
				applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))
			}
			testNamespace = os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV)
			if len(testNamespace) > 0 {
				asAdminClient, err := kubeapi.NewAdminKubernetesClient()
				Expect(err).ShouldNot(HaveOccurred())
				kubeadminClient, err = framework.InitControllerHub(asAdminClient)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = kubeadminClient.CommonController.CreateTestNamespace(testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
			} else {
				f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
				Expect(err).NotTo(HaveOccurred())
				testNamespace = f.UserNamespace
				Expect(f.UserNamespace).NotTo(BeNil())
				kubeadminClient = f.AsKubeAdmin
			}

			_, err = kubeadminClient.HasController.GetApplication(applicationName, testNamespace)
			// In case the app with the same name exist in the selected namespace, delete it first
			if err == nil {
				Expect(kubeadminClient.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Eventually(func() bool {
					_, err := kubeadminClient.HasController.GetApplication(applicationName, testNamespace)
					return errors.IsNotFound(err)
				}, time.Minute*5, time.Second*1).Should(BeTrue(), fmt.Sprintf("timed out when waiting for the app %s to be deleted in %s namespace", applicationName, testNamespace))
			}
			_, err = kubeadminClient.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			for _, gitUrl := range componentUrls {
				gitUrl := gitUrl
				componentName = fmt.Sprintf("%s-%s", "test-comp", util.GenerateRandomString(4))
				name := CreateComponent(kubeadminClient.HasController, gitUrl, "", applicationName, componentName, testNamespace)
				Expect(name).ShouldNot(BeEmpty())
				componentNames = append(componentNames, name)
			}

			// Create component for the repo containing symlink
			symlinkComponentName = fmt.Sprintf("%s-%s", "test-symlink-comp", util.GenerateRandomString(4))
			symlinkComponentName = CreateComponent(
				kubeadminClient.HasController, pythonComponentGitSourceURL, gitRepoContainsSymlinkBranchName,
				applicationName, symlinkComponentName, testNamespace)
		})

		AfterAll(func() {
			//Remove finalizers from pipelineruns
			Eventually(func() error {
				pipelineRuns, err := kubeadminClient.HasController.GetAllPipelineRunsForApplication(applicationName, testNamespace)
				if err != nil {
					GinkgoWriter.Printf("error while getting pipelineruns: %v\n", err)
					return err
				}
				for i := 0; i < len(pipelineRuns.Items); i++ {
					if utils.Contains(pipelineRunsWithE2eFinalizer, pipelineRuns.Items[i].GetName()) {
						err = kubeadminClient.TektonController.RemoveFinalizerFromPipelineRun(&pipelineRuns.Items[i], constants.E2ETestFinalizerName)
						if err != nil {
							GinkgoWriter.Printf("error removing e2e test finalizer from %s : %v\n", pipelineRuns.Items[i].GetName(), err)
							return err
						}
					}
				}
				return nil
			}, time.Minute*1, time.Second*10).Should(Succeed(), "timed out when trying to remove the e2e-test finalizer from pipelineruns")
			// Do cleanup only in case the test succeeded
			if !CurrentSpecReport().Failed() {
				// Clean up only Application CR (Component and Pipelines are included) in case we are targeting specific namespace
				// Used e.g. in build-definitions e2e tests, where we are targeting build-templates-e2e namespace
				if os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV) != "" {
					DeferCleanup(kubeadminClient.HasController.DeleteApplication, applicationName, testNamespace, false)
				} else {
					Expect(kubeadminClient.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
					Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
				}
			}
		})

		It(fmt.Sprintf("triggers PipelineRun for symlink component with source URL %s", pythonComponentGitSourceURL), Label(buildTemplatesTestLabel, sourceBuildTestLabel), func() {
			timeout := time.Minute * 5
			prName := WaitForPipelineRunStarts(kubeadminClient, applicationName, symlinkComponentName, testNamespace, timeout)
			Expect(prName).ShouldNot(BeEmpty())
			pipelineRunsWithE2eFinalizer = append(pipelineRunsWithE2eFinalizer, prName)
		})

		for i, gitUrl := range componentUrls {
			i := i
			gitUrl := gitUrl
			It(fmt.Sprintf("triggers PipelineRun for component with source URL %s", gitUrl), Label(buildTemplatesTestLabel, sourceBuildTestLabel), func() {
				timeout := time.Minute * 5
				prName := WaitForPipelineRunStarts(kubeadminClient, applicationName, componentNames[i], testNamespace, timeout)
				Expect(prName).ShouldNot(BeEmpty())
				pipelineRunsWithE2eFinalizer = append(pipelineRunsWithE2eFinalizer, prName)
			})
		}

		for i, gitUrl := range componentUrls {
			i := i
			gitUrl := gitUrl
			var pr *tektonpipeline.PipelineRun

			It(fmt.Sprintf("should eventually finish successfully for component with Git source URL %s", gitUrl), Label(buildTemplatesTestLabel, sourceBuildTestLabel), func() {
				component, err := kubeadminClient.HasController.GetComponent(componentNames[i], testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(kubeadminClient.HasController.WaitForComponentPipelineToBeFinished(component, "",
					kubeadminClient.TektonController, &has.RetryOptions{Retries: pipelineCompletionRetries, Always: true}, nil)).To(Succeed())
			})

			It(fmt.Sprintf("should ensure SBOM is shown for component with Git source URL %s", gitUrl), Label(buildTemplatesTestLabel), func() {
				pr, err = kubeadminClient.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(pr).ToNot(BeNil(), fmt.Sprintf("PipelineRun for the component %s/%s not found", testNamespace, componentNames[i]))

				logs, err := kubeadminClient.TektonController.GetTaskRunLogs(pr.GetName(), "show-sbom", testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(logs).To(HaveLen(1))
				var sbomTaskLog string
				for _, log := range logs {
					sbomTaskLog = log
				}

				sbom := &build.SbomCyclonedx{}
				GinkgoWriter.Printf("sbom task log: %s\n", sbomTaskLog)

				err = json.Unmarshal([]byte(sbomTaskLog), sbom)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to parse SBOM from show-sbom task output from %s/%s PipelineRun", pr.GetNamespace(), pr.GetName()))
				Expect(sbom.BomFormat).ToNot(BeEmpty())
				Expect(sbom.SpecVersion).ToNot(BeEmpty())
				Expect(sbom.Components).ToNot(BeEmpty())
			})

			It(fmt.Sprintf("should ensure show-summary task ran for component with Git source URL %s", gitUrl), Label(buildTemplatesTestLabel), func() {
				pr, err = kubeadminClient.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(pr).ToNot(BeNil(), fmt.Sprintf("PipelineRun for the component %s/%s not found", testNamespace, componentNames[i]))

				logs, err := kubeadminClient.TektonController.GetTaskRunLogs(pr.GetName(), "show-summary", testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(logs).To(HaveLen(1))
				buildSummaryLog := logs["step-appstudio-summary"]
				binaryImage := build.GetBinaryImage(pr)
				Expect(buildSummaryLog).To(ContainSubstring(binaryImage))
			})

			It("floating tags are created successfully", func() {
				_, _, _, _, _, checkAdditionalTags := GetComponentScenarioDetailsFromGitUrl(gitUrl)
				if !checkAdditionalTags {
					Skip(fmt.Sprintf("floating tag validation is not needed for: %s", gitUrl))
				}
				builtImage := build.GetBinaryImage(pr)
				Expect(builtImage).ToNot(BeEmpty(), "built image url is empty")
				builtImageRef, err := reference.Parse(builtImage)
				Expect(err).ShouldNot(HaveOccurred(),
					fmt.Sprintf("cannot parse image pullspec: %s", builtImage))
				for _, tagName := range additionalTags {
					_, err := build.GetImageTag(builtImageRef.Namespace, builtImageRef.Name, tagName)
					Expect(err).ShouldNot(HaveOccurred(),
						fmt.Sprintf("failed to get tag %s from image repo", tagName),
					)
				}
			})

			It("check for source images if enabled in pipeline", Label(buildTemplatesTestLabel, sourceBuildTestLabel), func() {
				pr, err = kubeadminClient.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(pr).ToNot(BeNil(), fmt.Sprintf("PipelineRun for the component %s/%s not found", testNamespace, componentNames[i]))

				if build.IsFBCBuild(pr) {
					GinkgoWriter.Println("This is FBC build, which does not require source container build.")
					Skip(fmt.Sprintf("Skiping FBC build %s", pr.GetName()))
					return
				}

				isSourceBuildEnabled := build.IsSourceBuildEnabled(pr)
				GinkgoWriter.Printf("Source build is enabled: %v\n", isSourceBuildEnabled)
				if !isSourceBuildEnabled {
					Skip("Skipping source image check since it is not enabled in the pipeline")
				}

				binaryImage := build.GetBinaryImage(pr)
				if binaryImage == "" {
					Fail("Failed to get the binary image url from pipelinerun")
				}

				binaryImageRef, err := reference.Parse(binaryImage)
				Expect(err).ShouldNot(HaveOccurred(),
					fmt.Sprintf("cannot parse binary image pullspec %s", binaryImage))

				tagInfo, err := build.GetImageTag(binaryImageRef.Namespace, binaryImageRef.Name, binaryImageRef.Tag)
				Expect(err).ShouldNot(HaveOccurred(),
					fmt.Sprintf("failed to get tag %s info for constructing source container image", binaryImageRef.Tag),
				)

				srcImageRef := reference.DockerImageReference{
					Registry:  binaryImageRef.Registry,
					Namespace: binaryImageRef.Namespace,
					Name:      binaryImageRef.Name,
					Tag:       fmt.Sprintf("%s.src", strings.Replace(tagInfo.ManifestDigest, ":", "-", 1)),
				}
				srcImage := srcImageRef.String()
				tagExists, err := build.DoesTagExistsInQuay(srcImage)
				Expect(err).ShouldNot(HaveOccurred(),
					fmt.Sprintf("failed to check existence of source container image %s", srcImage))
				Expect(tagExists).To(BeTrue(),
					fmt.Sprintf("cannot find source container image %s", srcImage))

				CheckSourceImage(srcImage, gitUrl, kubeadminClient, pr)
			})

			When(fmt.Sprintf("Pipeline Results are stored for component with Git source URL %s", gitUrl), Label("pipeline"), func() {
				var resultClient *pipeline.ResultClient
				var pr *tektonpipeline.PipelineRun

				BeforeAll(func() {
					// create the proxyplugin for tekton-results
					_, err = kubeadminClient.CommonController.CreateProxyPlugin("tekton-results", "toolchain-host-operator", "tekton-results", "tekton-results")
					Expect(err).NotTo(HaveOccurred())

					regProxyUrl := fmt.Sprintf("%s/plugins/tekton-results", f.ProxyUrl)
					resultClient = pipeline.NewClient(regProxyUrl, f.UserToken)

					pr, err = kubeadminClient.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, "")
					Expect(err).ShouldNot(HaveOccurred())
				})

				AfterAll(func() {
					Expect(kubeadminClient.CommonController.DeleteProxyPlugin("tekton-results", "toolchain-host-operator")).To(BeTrue())
				})

				It("should have Pipeline Records", func() {
					records, err := resultClient.GetRecords(testNamespace, string(pr.GetUID()))
					// temporary logs due to RHTAPBUGS-213
					GinkgoWriter.Printf("records for PipelineRun %s:\n%s\n", pr.Name, records)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("got error getting records for PipelineRun %s: %v", pr.Name, err))
					Expect(records.Record).NotTo(BeEmpty(), fmt.Sprintf("No records found for PipelineRun %s", pr.Name))
				})

				// Temporarily disabled until https://issues.redhat.com/browse/SRVKP-4348 is resolved
				It("should have Pipeline Logs", Pending, func() {
					// Verify if result is stored in Database
					// temporary logs due to RHTAPBUGS-213
					logs, err := resultClient.GetLogs(testNamespace, string(pr.GetUID()))
					GinkgoWriter.Printf("logs for PipelineRun %s:\n%s\n", pr.GetName(), logs)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("got error getting logs for PipelineRun %s: %v", pr.Name, err))

					timeout := time.Minute * 2
					interval := time.Second * 10
					// temporary timeout  due to RHTAPBUGS-213
					Eventually(func() error {
						// temporary logs due to RHTAPBUGS-213
						logs, err = resultClient.GetLogs(testNamespace, string(pr.GetUID()))
						if err != nil {
							return fmt.Errorf("failed to get logs for PipelineRun %s: %v", pr.Name, err)
						}
						GinkgoWriter.Printf("logs for PipelineRun %s:\n%s\n", pr.Name, logs)

						if len(logs.Record) == 0 {
							return fmt.Errorf("logs for PipelineRun %s/%s are empty", pr.GetNamespace(), pr.GetName())
						}
						return nil
					}, timeout, interval).Should(Succeed(), fmt.Sprintf("timed out when getting logs for PipelineRun %s/%s", pr.GetNamespace(), pr.GetName()))

					// Verify if result is stored in S3
					// temporary logs due to RHTAPBUGS-213
					log, err := resultClient.GetLogByName(logs.Record[0].Name)
					GinkgoWriter.Printf("log for record %s:\n%s\n", logs.Record[0].Name, log)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("got error getting log '%s' for PipelineRun %s: %v", logs.Record[0].Name, pr.GetName(), err))
					Expect(log).NotTo(BeEmpty(), fmt.Sprintf("no log content '%s' found for PipelineRun %s", logs.Record[0].Name, pr.GetName()))
				})
			})

			It(fmt.Sprintf("should validate tekton taskrun test results for component with Git source URL %s", gitUrl), Label(buildTemplatesTestLabel), func() {
				pr, err := kubeadminClient.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(build.ValidateBuildPipelineTestResults(pr, kubeadminClient.CommonController.KubeRest())).To(Succeed())
			})

			When(fmt.Sprintf("the container image for component with Git source URL %s is created and pushed to container registry", gitUrl), Label("sbom", "slow"), func() {
				var imageWithDigest string
				var pr *tektonpipeline.PipelineRun

				BeforeAll(func() {
					var err error
					imageWithDigest, err = getImageWithDigest(kubeadminClient, componentNames[i], applicationName, testNamespace)
					Expect(err).NotTo(HaveOccurred())
				})
				AfterAll(func() {
					if !CurrentSpecReport().Failed() {
						Expect(kubeadminClient.TektonController.DeletePipelineRun(pr.GetName(), pr.GetNamespace())).To(Succeed())
					}
				})

				It("verify-enterprise-contract check should pass", Label(buildTemplatesTestLabel), func() {
					// If the Tekton Chains controller is busy, it may take longer than usual for it
					// to sign and attest the image built in BeforeAll.
					err = kubeadminClient.TektonController.AwaitAttestationAndSignature(imageWithDigest, constants.ChainsAttestationTimeout)
					Expect(err).ToNot(HaveOccurred())

					cm, err := kubeadminClient.CommonController.GetConfigMap("ec-defaults", "enterprise-contract-service")
					Expect(err).ToNot(HaveOccurred())

					verifyECTaskBundle := cm.Data["verify_ec_task_bundle"]
					Expect(verifyECTaskBundle).ToNot(BeEmpty())

					publicSecretName := "cosign-public-key"
					publicKey, err := kubeadminClient.TektonController.GetTektonChainsPublicKey()
					Expect(err).ToNot(HaveOccurred())

					Expect(kubeadminClient.TektonController.CreateOrUpdateSigningSecret(
						publicKey, publicSecretName, testNamespace)).To(Succeed())

					defaultECP, err := kubeadminClient.TektonController.GetEnterpriseContractPolicy("default", "enterprise-contract-service")
					Expect(err).NotTo(HaveOccurred())

					policy := contract.PolicySpecWithSourceConfig(
						defaultECP.Spec,
						ecp.SourceConfig{
							Include: []string{"@slsa3"},
							Exclude: []string{"cve"},
						},
					)
					Expect(kubeadminClient.TektonController.CreateOrUpdatePolicyConfiguration(testNamespace, policy)).To(Succeed())

					pipelineRun, err := kubeadminClient.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, "")
					Expect(err).ToNot(HaveOccurred())

					revision := pipelineRun.Annotations["build.appstudio.redhat.com/commit_sha"]
					Expect(revision).ToNot(BeEmpty())

					generator := tekton.VerifyEnterpriseContract{
						Snapshot: v1alpha1.SnapshotSpec{
							Application: applicationName,
							Components: []v1alpha1.SnapshotComponent{
								{
									Name:           componentNames[i],
									ContainerImage: imageWithDigest,
									Source: v1alpha1.ComponentSource{
										ComponentSourceUnion: v1alpha1.ComponentSourceUnion{
											GitSource: &v1alpha1.GitSource{
												URL:      gitUrl,
												Revision: revision,
											},
										},
									},
								},
							},
						},
						TaskBundle:          verifyECTaskBundle,
						Name:                "verify-enterprise-contract",
						Namespace:           testNamespace,
						PolicyConfiguration: "ec-policy",
						PublicKey:           fmt.Sprintf("k8s://%s/%s", testNamespace, publicSecretName),
						Strict:              true,
						EffectiveTime:       "now",
						IgnoreRekor:         true,
					}

					pr, err = kubeadminClient.TektonController.RunPipeline(generator, testNamespace, int(ecPipelineRunTimeout.Seconds()))
					Expect(err).NotTo(HaveOccurred())

					Expect(kubeadminClient.TektonController.WatchPipelineRun(pr.Name, testNamespace, int(ecPipelineRunTimeout.Seconds()))).To(Succeed())

					pr, err = kubeadminClient.TektonController.GetPipelineRun(pr.Name, pr.Namespace)
					Expect(err).NotTo(HaveOccurred())

					tr, err := kubeadminClient.TektonController.GetTaskRunStatus(kubeadminClient.CommonController.KubeRest(), pr, "verify-enterprise-contract")
					Expect(err).NotTo(HaveOccurred())
					Expect(tekton.DidTaskRunSucceed(tr)).To(BeTrue())
					Expect(tr.Status.TaskRunStatusFields.Results).Should(Or(
						// TODO: delete the first option after https://issues.redhat.com/browse/RHTAP-810 is completed
						ContainElements(tekton.MatchTaskRunResultWithJSONPathValue(constants.OldTektonTaskTestOutputName, "{$.result}", `["SUCCESS"]`)),
						ContainElements(tekton.MatchTaskRunResultWithJSONPathValue(constants.TektonTaskTestOutputName, "{$.result}", `["SUCCESS"]`)),
					))
				})
				It("contains non-empty sbom files", Label(buildTemplatesTestLabel), func() {
					purl, cyclonedx, err := build.GetParsedSbomFilesContentFromImage(imageWithDigest)
					Expect(err).NotTo(HaveOccurred())

					Expect(cyclonedx.BomFormat).To(Equal("CycloneDX"))
					Expect(cyclonedx.SpecVersion).ToNot(BeEmpty())
					Expect(cyclonedx.Version).ToNot(BeZero())
					Expect(cyclonedx.Components).ToNot(BeEmpty())

					numberOfLibraryComponents := 0
					for _, component := range cyclonedx.Components {
						Expect(component.Name).ToNot(BeEmpty())
						Expect(component.Type).ToNot(BeEmpty())

						if component.Type == "library" || component.Type == "application" {
							Expect(component.Purl).ToNot(BeEmpty())
							numberOfLibraryComponents++
						}
					}

					Expect(purl.ImageContents.Dependencies).ToNot(BeEmpty())
					Expect(purl.ImageContents.Dependencies).To(HaveLen(numberOfLibraryComponents))

					for _, dependency := range purl.ImageContents.Dependencies {
						Expect(dependency.Purl).ToNot(BeEmpty())
					}
				})
			})

			Context("build-definitions ec pipelines", Label(buildTemplatesTestLabel), func() {
				ecPipelines := []string{
					"pipelines/enterprise-contract.yaml",
				}

				var gitRevision, gitURL, imageWithDigest string

				BeforeAll(func() {
					// resolve the gitURL and gitRevision
					var err error
					gitURL, gitRevision, err = build.ResolveGitDetails(constants.EC_PIPELINES_REPO_URL_ENV, constants.EC_PIPELINES_REPO_REVISION_ENV)
					Expect(err).NotTo(HaveOccurred())

					// Double check that the component has finished. There's an earlier test that
					// verifies this so this should be a no-op. It is added here in order to avoid
					// unnecessary coupling of unrelated tests.
					component, err := kubeadminClient.HasController.GetComponent(componentNames[i], testNamespace)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(kubeadminClient.HasController.WaitForComponentPipelineToBeFinished(
						component, "", kubeadminClient.TektonController, &has.RetryOptions{Retries: pipelineCompletionRetries, Always: true}, nil)).To(Succeed())

					imageWithDigest, err = getImageWithDigest(kubeadminClient, componentNames[i], applicationName, testNamespace)
					Expect(err).NotTo(HaveOccurred())

					err = kubeadminClient.TektonController.AwaitAttestationAndSignature(imageWithDigest, constants.ChainsAttestationTimeout)
					Expect(err).NotTo(HaveOccurred())
				})

				for _, pathInRepo := range ecPipelines {
					pathInRepo := pathInRepo
					It(fmt.Sprintf("runs ec pipeline %s", pathInRepo), func() {
						generator := tekton.ECIntegrationTestScenario{
							Image:                 imageWithDigest,
							Namespace:             testNamespace,
							PipelineGitURL:        gitURL,
							PipelineGitRevision:   gitRevision,
							PipelineGitPathInRepo: pathInRepo,
						}

						pr, err := kubeadminClient.TektonController.RunPipeline(generator, testNamespace, int(ecPipelineRunTimeout.Seconds()))
						Expect(err).NotTo(HaveOccurred())
						defer func(pr *tektonpipeline.PipelineRun) {
							// Avoid blowing up PipelineRun usage
							err := kubeadminClient.TektonController.DeletePipelineRun(pr.Name, pr.Namespace)
							Expect(err).NotTo(HaveOccurred())
						}(pr)
						Expect(kubeadminClient.TektonController.WatchPipelineRun(pr.Name, testNamespace, int(ecPipelineRunTimeout.Seconds()))).To(Succeed())

						// Refresh our copy of the PipelineRun for latest results
						pr, err = kubeadminClient.TektonController.GetPipelineRun(pr.Name, pr.Namespace)
						Expect(err).NotTo(HaveOccurred())

						// The UI uses this label to display additional information.
						Expect(pr.Labels["build.appstudio.redhat.com/pipeline"]).To(Equal("enterprise-contract"))

						// The UI uses this label to display additional information.
						tr, err := kubeadminClient.TektonController.GetTaskRunFromPipelineRun(kubeadminClient.CommonController.KubeRest(), pr, "verify")
						Expect(err).NotTo(HaveOccurred())
						Expect(tr.Labels["build.appstudio.redhat.com/pipeline"]).To(Equal("enterprise-contract"))

						logs, err := kubeadminClient.TektonController.GetTaskRunLogs(pr.Name, "verify", pr.Namespace)
						Expect(err).NotTo(HaveOccurred())

						// The logs from the report step are used by the UI to display validation
						// details. Let's make sure it has valid YAML.
						reportLogs := logs["step-report"]
						Expect(reportLogs).NotTo(BeEmpty())
						var reportYAML any
						err = yaml.Unmarshal([]byte(reportLogs), &reportYAML)
						Expect(err).NotTo(HaveOccurred())

						// The logs from the summary step are used by the UI to display an overview of
						// the validation.
						summaryLogs := logs["step-summary"]
						GinkgoWriter.Printf("got step-summary log: %s\n", summaryLogs)
						Expect(summaryLogs).NotTo(BeEmpty())
						var summary build.TestOutput
						err = json.Unmarshal([]byte(summaryLogs), &summary)
						Expect(err).NotTo(HaveOccurred())
						Expect(summary).NotTo(Equal(build.TestOutput{}))
					})
				}
			})
		}

		It(fmt.Sprintf("pipelineRun should fail for symlink component with Git source URL %s", pythonComponentGitSourceURL), Label(buildTemplatesTestLabel, sourceBuildTestLabel), func() {
			component, err := kubeadminClient.HasController.GetComponent(symlinkComponentName, testNamespace)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(kubeadminClient.HasController.WaitForComponentPipelineToBeFinished(component, "",
				kubeadminClient.TektonController, &has.RetryOptions{Retries: pipelineCompletionRetries}, nil)).Should(MatchError(ContainSubstring("cloned repository contains symlink pointing outside of the cloned repository")))
		})
	})
})

func getImageWithDigest(c *framework.ControllerHub, componentName, applicationName, namespace string) (string, error) {
	var url string
	var digest string
	pipelineRun, err := c.HasController.GetComponentPipelineRun(componentName, applicationName, namespace, "")
	if err != nil {
		return "", err
	}

	for _, p := range pipelineRun.Spec.Params {
		if p.Name == "output-image" {
			url = p.Value.StringVal
		}
	}
	if url == "" {
		return "", fmt.Errorf("output-image of a component %q could not be found", componentName)
	}

	for _, r := range pipelineRun.Status.PipelineRunStatusFields.Results {
		if r.Name == "IMAGE_DIGEST" {
			digest = r.Value.StringVal
		}
	}
	if digest == "" {
		return "", fmt.Errorf("IMAGE_DIGEST for component %q could not be found", componentName)
	}
	return fmt.Sprintf("%s@%s", url, digest), nil
}

// this function takes a bundle and prefetchInput value as inputs and creates a bundle with param hermetic=true
// and then push the bundle to quay using format: quay.io/<QUAY_E2E_ORGANIZATION>/test-images:<generated_tag>
func enableHermeticBuildInPipelineBundle(customDockerBuildBundle, prefetchInput string) (string, error) {
	var tektonObj runtime.Object
	var err error
	var newPipelineYaml []byte
	// Extract docker-build pipeline as tekton object from the bundle
	if tektonObj, err = tekton.ExtractTektonObjectFromBundle(customDockerBuildBundle, "pipeline", "docker-build"); err != nil {
		return "", fmt.Errorf("failed to extract the Tekton Pipeline from bundle: %+v", err)
	}
	dockerPipelineObject := tektonObj.(*tektonpipeline.Pipeline)
	// Update hermetic params value to true and also update prefetch-input param value
	for i := range dockerPipelineObject.PipelineSpec().Params {
		if dockerPipelineObject.PipelineSpec().Params[i].Name == "hermetic" {
			dockerPipelineObject.PipelineSpec().Params[i].Default.StringVal = "true"
		}
		if dockerPipelineObject.PipelineSpec().Params[i].Name == "prefetch-input" {
			dockerPipelineObject.PipelineSpec().Params[i].Default.StringVal = prefetchInput
		}
	}
	if newPipelineYaml, err = yaml.Marshal(dockerPipelineObject); err != nil {
		return "", fmt.Errorf("error when marshalling a new pipeline to YAML: %v", err)
	}
	keychain := authn.NewMultiKeychain(authn.DefaultKeychain)
	authOption := remoteimg.WithAuthFromKeychain(keychain)
	tag := fmt.Sprintf("%d-%s", time.Now().Unix(), util.GenerateRandomString(4))
	quayOrg := utils.GetEnv(constants.QUAY_E2E_ORGANIZATION_ENV, constants.DefaultQuayOrg)
	newDockerBuildPipelineImg := strings.ReplaceAll(constants.DefaultImagePushRepo, constants.DefaultQuayOrg, quayOrg)
	var newDockerBuildPipeline, _ = name.ParseReference(fmt.Sprintf("%s:pipeline-bundle-%s", newDockerBuildPipelineImg, tag))
	// Build and Push the tekton bundle
	if err = tekton.BuildAndPushTektonBundle(newPipelineYaml, newDockerBuildPipeline, authOption); err != nil {
		return "", fmt.Errorf("error when building/pushing a tekton pipeline bundle: %v", err)
	}
	return newDockerBuildPipeline.String(), nil
}

// this function takes a bundle and additonalTags string slice as inputs
// and creates a bundle with adding ADDITIONAL_TAGS params in the apply-tags task
// and then push the bundle to quay using format: quay.io/<QUAY_E2E_ORGANIZATION>/test-images:<generated_tag>
func applyAdditionalTagsInPipelineBundle(customDockerBuildBundle string, additionalTags []string) (string, error) {
	var tektonObj runtime.Object
	var err error
	var newPipelineYaml []byte
	// Extract docker-build pipeline as tekton object from the bundle
	if tektonObj, err = tekton.ExtractTektonObjectFromBundle(customDockerBuildBundle, "pipeline", "docker-build"); err != nil {
		return "", fmt.Errorf("failed to extract the Tekton Pipeline from bundle: %+v", err)
	}
	dockerPipelineObject := tektonObj.(*tektonpipeline.Pipeline)
	// Update ADDITIONAL_TAGS params arrays with additionalTags in apply-tags task
	for i := range dockerPipelineObject.PipelineSpec().Tasks {
		t := &dockerPipelineObject.PipelineSpec().Tasks[i]
		if t.Name == "apply-tags" {
			t.Params = append(t.Params, tektonpipeline.Param{Name: "ADDITIONAL_TAGS", Value: *tektonpipeline.NewStructuredValues(additionalTags[0], additionalTags[1:]...)})
		}
	}

	if newPipelineYaml, err = yaml.Marshal(dockerPipelineObject); err != nil {
		return "", fmt.Errorf("error when marshalling a new pipeline to YAML: %v", err)
	}
	keychain := authn.NewMultiKeychain(authn.DefaultKeychain)
	authOption := remoteimg.WithAuthFromKeychain(keychain)
	tag := fmt.Sprintf("%d-%s", time.Now().Unix(), util.GenerateRandomString(4))
	quayOrg := utils.GetEnv(constants.QUAY_E2E_ORGANIZATION_ENV, constants.DefaultQuayOrg)
	newDockerBuildPipelineImg := strings.ReplaceAll(constants.DefaultImagePushRepo, constants.DefaultQuayOrg, quayOrg)
	var newDockerBuildPipeline, _ = name.ParseReference(fmt.Sprintf("%s:pipeline-bundle-%s", newDockerBuildPipelineImg, tag))
	// Build and Push the tekton bundle
	if err = tekton.BuildAndPushTektonBundle(newPipelineYaml, newDockerBuildPipeline, authOption); err != nil {
		return "", fmt.Errorf("error when building/pushing a tekton pipeline bundle: %v", err)
	}
	return newDockerBuildPipeline.String(), nil
}
