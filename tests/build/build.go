package build

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/devfile/library/pkg/util"
	"github.com/google/uuid"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/build"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	klog "k8s.io/klog/v2"

	routev1 "github.com/openshift/api/route/v1"
)

const (
	containerImageSource                 = "quay.io/redhat-appstudio-qe/busybox-loop:latest"
	helloWorldComponentGitSourceRepoName = "devfile-sample-hello-world"
	helloWorldComponentGitSourceURL      = "https://github.com/redhat-appstudio-qe/" + helloWorldComponentGitSourceRepoName
	pythonComponentGitSourceURL          = "https://github.com/redhat-appstudio-qe/devfile-sample-python-basic"
	dummyPipelineBundleRef               = "quay.io/redhat-appstudio-qe/dummy-pipeline-bundle:latest"
)

var (
	componentUrls  = strings.Split(utils.GetEnv(COMPONENT_REPO_URLS_ENV, pythonComponentGitSourceURL), ",") //multiple urls
	componentNames []string
)

var _ = framework.BuildSuiteDescribe("Build service E2E tests", func() {

	f, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	Describe("Component with git source is created", Ordered, func() {
		var applicationName, componentName, testNamespace, outputContainerImage string

		var timeout, interval time.Duration

		var webhookRoute *routev1.Route
		var webhookURL string

		BeforeAll(func() {

			applicationName = fmt.Sprintf("build-suite-test-application-%s", util.GenerateRandomString(4))
			testNamespace = fmt.Sprintf("e2e-test-%s", util.GenerateRandomString(4))

			_, err = f.CommonController.CreateTestNamespace(testNamespace)
			Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", testNamespace, err)

			_, err = f.HasController.CreateHasApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			componentName = fmt.Sprintf("build-suite-test-component-git-source-%s", util.GenerateRandomString(4))
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
			timeout = time.Second * 120
			interval = time.Second * 1
			// Create a component with Git Source URL being defined
			_, err := f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())
			DeferCleanup(f.CommonController.DeleteNamespace, testNamespace)
		})

		It("triggers a PipelineRun", func() {
			Eventually(func() bool {
				pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
				if err != nil {
					klog.Infoln("PipelineRun has not been created yet")
					return false
				}
				return pipelineRun.HasStarted()
			}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to start")
		})

		When("the PipelineRun has started", func() {
			BeforeAll(func() {
				timeout = time.Second * 600
				interval = time.Second * 10
			})
			It("should eventually finish successfully", func() {
				Eventually(func() bool {
					pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
					Expect(err).ShouldNot(HaveOccurred())

					for _, condition := range pipelineRun.Status.Conditions {
						klog.Infof("PipelineRun %s Status.Conditions.Reason: %s\n", pipelineRun.Name, condition.Reason)

						if condition.Reason == string(v1beta1.PipelineRunReasonFailed) {
							d := utils.GetFailedPipelineRunDetails(pipelineRun)
							logs, _ := f.CommonController.GetContainerLogs(d.PodName, d.FailedContainerName, testNamespace)
							Fail(fmt.Sprintf("Pipelinerun '%s' has failed. Logs from failed container '%s': \n%s", pipelineRun.Name, d.FailedContainerName, logs))
						}
					}
					return pipelineRun.IsDone()
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to finish")
			})
		})

		When("the PipelineRun is finished", func() {
			var webhookName string

			BeforeAll(func() {
				timeout = time.Minute * 5
				interval = time.Second * 10
				webhookName = "el" + componentName

			})

			It("eventually leads to a creation of a component webhook (event listener)", Label("webhook", "slow"), func() {
				Eventually(func() bool {
					_, err := f.HasController.GetEventListenerRoute(componentName, testNamespace)
					if err != nil {
						klog.Infof("component webhook %s has not been created yet in %s namespace\n", webhookName, testNamespace)
						return false
					}
					return true

				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the component webhook to be created")
			})
		})

		When("the container image is created and pushed to container registry", Label("sbom", "slow"), func() {
			It("contains non-empty sbom files", func() {
				component, err := f.HasController.GetHasComponent(componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				purl, cyclonedx, err := build.GetParsedSbomFilesContentFromImage(component.Spec.ContainerImage)
				Expect(err).NotTo(HaveOccurred())
				Expect(purl.ImageContents.Dependencies).ToNot(BeEmpty())
				Expect(cyclonedx.Components).ToNot(BeEmpty())
			})
		})
		When("the component event listener is created", Label("webhook", "slow"), func() {
			BeforeAll(func() {
				timeout = time.Minute * 1
				interval = time.Second * 1

				webhookRoute, err = f.HasController.GetEventListenerRoute(componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(webhookRoute.Spec.Host).ShouldNot(BeEmpty())

				if webhookRoute.Spec.TLS != nil {
					webhookURL = fmt.Sprintf("https://%s", webhookRoute.Spec.Host)
				} else {
					webhookURL = fmt.Sprintf("http://%s", webhookRoute.Spec.Host)
				}
			})

			It("should be eventually ready to receive events from Github", func() {
				Eventually(func() bool {
					c := http.Client{}
					req, err := http.NewRequestWithContext(context.Background(), "GET", webhookURL, bytes.NewReader([]byte("{}")))
					if err != nil {
						return false
					}
					resp, err := c.Do(req)
					if err != nil || resp.StatusCode > 299 {
						return false
					}
					return true
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the event listener to be ready")
			})
		})

		When("the component webhook is configured on Github repository for 'push' events and a change is pushed", Label("webhook", "slow"), func() {
			var initialContainerImageURL, newContainerImageURL string
			var webhookID int64

			BeforeAll(func() {
				if utils.IsPrivateHostname(webhookRoute.Spec.Host) {
					// Workaround for cleanup is needed because of the issue in ginkgo: https://github.com/onsi/ginkgo/issues/980
					DeferCleanup(f.CommonController.DeleteNamespace, testNamespace)
					Skip("Using private cluster (not reachable from Github), skipping...")
				}

				timeout = time.Minute * 2
				interval = time.Second * 5

				// Get Component CR to check the current value of Container Image
				component, err := f.HasController.GetHasComponent(componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				initialContainerImageURL = component.Spec.ContainerImage

				webhookID, err = f.CommonController.Github.CreateWebhook(helloWorldComponentGitSourceRepoName, webhookURL)
				Expect(err).ShouldNot(HaveOccurred())
				DeferCleanup(f.CommonController.Github.DeleteWebhook, helloWorldComponentGitSourceRepoName, webhookID)

				// Wait until EventListener's pod is ready to receive events
				err = f.CommonController.WaitForPodSelector(f.CommonController.IsPodRunning, testNamespace, "eventlistener", componentName, 60, 100)
				Expect(err).NotTo(HaveOccurred())

				// Update a file in Component source Github repo to trigger a push event
				updatedFile, err := f.CommonController.Github.UpdateFile(helloWorldComponentGitSourceRepoName, "README.md", "test")
				Expect(err).NotTo(HaveOccurred())

				// A new output container image URL should contain the suffix with the latest commit SHA
				newContainerImageURL = fmt.Sprintf("%s-%s", initialContainerImageURL, updatedFile.GetSHA())
			})

			It("eventually leads to triggering another PipelineRun", func() {
				Eventually(func() bool {
					pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, true)
					if err != nil {
						klog.Infoln("PipelineRun has not been created yet")
						return false
					}
					return pipelineRun.HasStarted()
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to start")
			})
			It("PipelineRun should eventually finish", func() {
				timeout = time.Minute * 5
				Eventually(func() bool {
					pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, true)
					Expect(err).ShouldNot(HaveOccurred())

					for _, condition := range pipelineRun.Status.Conditions {
						klog.Infof("PipelineRun %s Status.Conditions.Reason: %s\n", pipelineRun.Name, condition.Reason)

						if condition.Reason == string(v1beta1.PipelineRunReasonFailed) {
							d := utils.GetFailedPipelineRunDetails(pipelineRun)
							logs, _ := f.CommonController.GetContainerLogs(d.PodName, d.FailedContainerName, testNamespace)
							Fail(fmt.Sprintf("Pipelinerun '%s' has failed. Logs from failed container '%s': \n%s", pipelineRun.Name, d.FailedContainerName, logs))
						}
					}
					return pipelineRun.IsDone()
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to finish")
			})
			It("ContainerImage field should be updated with the SHA of the commit that was pushed to the Component source repo", func() {
				Eventually(func() bool {
					component, err := f.HasController.GetHasComponent(componentName, testNamespace)
					if err != nil {
						klog.Infoln("component was not updated yet")
						return false
					}
					return component.Spec.ContainerImage == newContainerImageURL
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the Component CR to get updated with a new container image URL")
			})
		})
	})

	Describe("HACBS pipelines", Ordered, func() {

		var applicationName, componentName, testNamespace, outputContainerImage string
		var defaultBundleConfigMap *v1.ConfigMap
		var defaultBundleRef, customBundleRef string

		BeforeAll(func() {
			if os.Getenv("APP_SUFFIX") != "" {
				applicationName = fmt.Sprintf("test-app-%s", os.Getenv("APP_SUFFIX"))
			} else {
				applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))
			}
			testNamespace = utils.GetEnv(constants.E2E_APPLICATIONS_NAMESPACE_ENV, fmt.Sprintf("e2e-test-pipelines-%s", util.GenerateRandomString(4)))

			_, err := f.CommonController.CreateTestNamespace(testNamespace)
			Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", testNamespace, err)

			_, err = f.HasController.GetHasApplication(applicationName, testNamespace)
			// In case the app with the same name exist in the selected namespace, delete it first
			if err == nil {
				Expect(f.HasController.DeleteHasApplication(applicationName, testNamespace)).To(Succeed())
				Eventually(func() bool {
					_, err := f.HasController.GetHasApplication(applicationName, testNamespace)
					return errors.IsNotFound(err)
				}, time.Minute*1, time.Second*1).Should(BeTrue(), "timed out when waiting for the app %s to be deleted in %s namespace", applicationName, testNamespace)
			}
			_, err = f.HasController.CreateHasApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			customBundleConfigMap, err := f.CommonController.GetConfigMap(constants.BuildPipelinesConfigMapName, testNamespace)
			if err != nil {
				if errors.IsNotFound(err) {
					defaultBundleConfigMap, err = f.CommonController.GetConfigMap(constants.BuildPipelinesConfigMapName, constants.BuildPipelinesConfigMapDefaultNamespace)
					Expect(err).ToNot(HaveOccurred())

					bundlePullSpec := defaultBundleConfigMap.Data["default_build_bundle"]
					hacbsBundleConfigMap := &v1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{Name: constants.BuildPipelinesConfigMapName},
						Data:       map[string]string{"default_build_bundle": strings.Replace(bundlePullSpec, "build-", "hacbs-", 1)},
					}
					_, err = f.CommonController.CreateConfigMap(hacbsBundleConfigMap, testNamespace)
					Expect(err).ToNot(HaveOccurred())
					DeferCleanup(f.CommonController.DeleteConfigMap, constants.BuildPipelinesConfigMapName, testNamespace)
				} else {
					Fail(fmt.Sprintf("error occured when trying to get configmap %s in %s namespace: %v", constants.BuildPipelinesConfigMapName, testNamespace, err))
				}
			} else {
				bundlePullSpec := customBundleConfigMap.Data["default_build_bundle"]
				hacbsBundleConfigMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: constants.BuildPipelinesConfigMapName},
					Data:       map[string]string{"default_build_bundle": strings.Replace(bundlePullSpec, "build-", "hacbs-", 1)},
				}

				_, err = f.CommonController.UpdateConfigMap(hacbsBundleConfigMap, testNamespace)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() error {
					hacbsBundleConfigMap.Data = customBundleConfigMap.Data
					_, err := f.CommonController.UpdateConfigMap(hacbsBundleConfigMap, testNamespace)
					if err != nil {
						return err
					}
					return nil
				})
			}

			for _, gitUrl := range componentUrls {
				gitUrl := gitUrl
				componentName = fmt.Sprintf("%s-%s", "test-component-", util.GenerateRandomString(4))
				componentNames = append(componentNames, componentName)
				outputContainerImage = fmt.Sprintf("quay.io/%s/test-images:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
				// Create a component with Git Source URL being defined
				_, err := f.HasController.CreateComponent(applicationName, componentName, testNamespace, gitUrl, "", outputContainerImage, "")
				Expect(err).ShouldNot(HaveOccurred())
			}
		})

		AfterAll(func() {
			// Do cleanup only in case the test succeeded
			if !CurrentSpecReport().Failed() {
				// Clean up only Application CR (Component and Pipelines are included) in case we are targeting specific namespace
				// Used e.g. in build-defintions e2e tests, where we are targeting build-templates-e2e namespace
				if os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV) != "" {
					DeferCleanup(f.HasController.DeleteHasApplication, applicationName, testNamespace)
				} else {
					Expect(f.CommonController.DeleteNamespace(testNamespace)).To(Succeed())
				}
			} else {
				// Workaround: We cannot keep applications/components present in the specific namespace due to
				// an issue reported here: https://issues.redhat.com/browse/PLNSRVCE-484
				// TODO: delete the whole 'else' block after the issue is resolved
				if os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV) != "" {
					DeferCleanup(f.HasController.DeleteHasApplication, applicationName, testNamespace)
				}
			}
		})

		for i, gitUrl := range componentUrls {
			gitUrl := gitUrl
			It(fmt.Sprintf("triggers PipelineRun for component with source URL %s", gitUrl), Label("build-templates-e2e"), func() {
				timeout := time.Second * 60
				interval := time.Second * 1

				Eventually(func() bool {
					pipelineRun, err := f.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, false)
					if err != nil {
						klog.Infoln("PipelineRun has not been created yet")
						return false
					}
					return pipelineRun.HasStarted()
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the %s PipelineRun to start", componentNames[i])
			})
		}

		It("should reference the custom pipeline bundle in a PipelineRun", Label("build-templates-e2e"), func() {
			customBundleConfigMap, err := f.CommonController.GetConfigMap(constants.BuildPipelinesConfigMapName, testNamespace)
			if err != nil {
				if errors.IsNotFound(err) {
					klog.Infof("configmap with custom pipeline bundle not found in %s namespace\n", testNamespace)
				} else {
					Fail(fmt.Sprintf("error occured when trying to get configmap %s in %s namespace: %v", constants.BuildPipelinesConfigMapName, testNamespace, err))
				}
			} else {
				customBundleRef = customBundleConfigMap.Data["default_build_bundle"]
			}

			if customBundleRef == "" {
				Skip("skipping the specs - custom pipeline bundle is not defined")
			}
			pipelineRun, err := f.HasController.GetComponentPipelineRun(componentNames[0], applicationName, testNamespace, false)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(pipelineRun.Spec.PipelineRef.Bundle).To(Equal(customBundleRef))
		})

		It("should reference the default pipeline bundle in a PipelineRun", func() {
			defaultBundleConfigMap, err = f.CommonController.GetConfigMap(constants.BuildPipelinesConfigMapName, constants.BuildPipelinesConfigMapDefaultNamespace)
			if err != nil {
				if errors.IsForbidden(err) {
					klog.Infof("don't have enough permissions to get a configmap with default pipeline in %s namespace\n", constants.BuildPipelinesConfigMapDefaultNamespace)
				} else {
					Fail(fmt.Sprintf("error occured when trying to get configmap %s in %s namespace: %v", constants.BuildPipelinesConfigMapName, constants.BuildPipelinesConfigMapDefaultNamespace, err))
				}
			} else {
				defaultBundleRef = defaultBundleConfigMap.Data["default_build_bundle"]
			}

			if customBundleRef != "" {
				Skip("skipping - custom pipeline bundle bundle (that overrides the default one) is defined")
			}
			if defaultBundleRef == "" {
				Skip("skipping - default pipeline bundle cannot be fetched")
			}
			pipelineRun, err := f.HasController.GetComponentPipelineRun(componentNames[0], applicationName, testNamespace, false)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(pipelineRun.Spec.PipelineRef.Bundle).To(Equal(defaultBundleRef))
		})

		for i, gitUrl := range componentUrls {
			gitUrl := gitUrl

			It(fmt.Sprintf("should eventually finish successfully for component with source URL %s", gitUrl), Label("build-templates-e2e"), func() {
				timeout := time.Second * 600
				interval := time.Second * 10
				Eventually(func() bool {
					pipelineRun, err := f.HasController.GetComponentPipelineRun(componentNames[i], applicationName, testNamespace, false)
					Expect(err).ShouldNot(HaveOccurred())

					for _, condition := range pipelineRun.Status.Conditions {
						klog.Infof("PipelineRun %s Status.Conditions.Reason: %s\n", pipelineRun.Name, condition.Reason)

						if condition.Reason == string(v1beta1.PipelineRunReasonFailed) {
							d := utils.GetFailedPipelineRunDetails(pipelineRun)
							logs, _ := f.CommonController.GetContainerLogs(d.PodName, d.FailedContainerName, testNamespace)
							Fail(fmt.Sprintf("Pipelinerun '%s' has failed. Logs from failed container '%s': \n%s", pipelineRun.Name, d.FailedContainerName, logs))
						}
					}
					return pipelineRun.IsDone()
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to finish")
			})
		}
	})

	Describe("Creating component with container image source", Ordered, func() {

		var applicationName, componentName, testNamespace string
		var timeout, interval time.Duration

		BeforeAll(func() {

			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))
			testNamespace = fmt.Sprintf("e2e-test-%s", util.GenerateRandomString(4))

			_, err = f.CommonController.CreateTestNamespace(testNamespace)
			Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", testNamespace, err)

			_, err = f.HasController.CreateHasApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(f.HasController.DeleteHasApplication, applicationName, testNamespace)

			componentName = fmt.Sprintf("build-suite-test-component-image-source-%s", util.GenerateRandomString(4))
			outputContainerImage := ""
			timeout = time.Second * 10
			interval = time.Second * 1
			// Create a component with containerImageSource being defined
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, "", containerImageSource, outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())

			DeferCleanup(f.CommonController.DeleteNamespace, testNamespace)
		})

		It("should not trigger a PipelineRun", func() {
			Consistently(func() bool {
				pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
				Expect(pipelineRun.Name).To(BeEmpty())

				return strings.Contains(err.Error(), "no pipelinerun found")
			}, timeout, interval).Should(BeTrue(), "expected the PipelineRun not to be triggered")
		})
	})

	Describe("Creating a configmap with 'dummy' custom pipeline bundle in the testing namespace", Ordered, func() {
		var timeout, interval time.Duration

		var componentName, applicationName, testNamespace string

		BeforeAll(func() {

			testNamespace := fmt.Sprintf("e2e-test-%s", util.GenerateRandomString(4))
			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))

			_, err = f.CommonController.CreateTestNamespace(testNamespace)
			Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", testNamespace, err)

			_, err = f.HasController.CreateHasApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: constants.BuildPipelinesConfigMapName},
				Data:       map[string]string{"default_build_bundle": dummyPipelineBundleRef},
			}
			_, err = f.CommonController.CreateConfigMap(cm, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			componentName = "build-suite-test-bundle-overriding"
			outputContainerImage := fmt.Sprintf("quay.io/%s/test-images:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())

			timeout = time.Second * 120
			interval = time.Second * 1

			DeferCleanup(f.CommonController.DeleteNamespace, testNamespace)
		})
		It("should be referenced in a PipelineRun", Label("build-bundle-overriding"), func() {
			Eventually(func() bool {
				pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
				if err != nil {
					klog.Infoln("PipelineRun has not been created yet")
					return false
				}
				return pipelineRun.HasStarted()
			}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to start")

			pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(pipelineRun.Spec.PipelineRef.Bundle).To(Equal(dummyPipelineBundleRef))
		})
	})

	Describe("A secret with dummy quay.io credentials is created in the testing namespace", Ordered, func() {

		var applicationName, componentName, testNamespace, outputContainerImage string
		var timeout, interval time.Duration

		BeforeAll(func() {

			testNamespace = fmt.Sprintf("e2e-test-%s", util.GenerateRandomString(4))

			_, err = f.CommonController.CreateTestNamespace(testNamespace)
			Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", testNamespace, err)

			_, err := f.CommonController.GetSecret(testNamespace, constants.RegistryAuthSecretName)
			if err != nil {
				// If we have an error when getting RegistryAuthSecretName, it should be IsNotFound err
				Expect(errors.IsNotFound(err)).To(BeTrue())
			} else {
				Skip("a registry auth secret is already created in testing namespace - skipping....")
			}

			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))

			_, err = f.HasController.CreateHasApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(f.HasController.DeleteHasApplication, applicationName, testNamespace)

			timeout = time.Minute * 2
			interval = time.Second * 1

			dummySecret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: constants.RegistryAuthSecretName},
				Type:       v1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{".dockerconfigjson": []byte("{\"auths\":{\"quay.io\":{\"username\":\"test\",\"password\":\"test\",\"auth\":\"dGVzdDp0ZXN0\",\"email\":\"\"}}}")},
			}

			_, err = f.CommonController.CreateSecret(testNamespace, dummySecret)
			Expect(err).ToNot(HaveOccurred())

			componentName = "build-suite-test-secret-overriding"
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())

			DeferCleanup(f.CommonController.DeleteNamespace, testNamespace)
		})

		It("should override the shared secret", func() {
			Eventually(func() bool {
				pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
				if err != nil {
					klog.Infoln("PipelineRun has not been created yet")
					return false
				}
				return pipelineRun.HasStarted()
			}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to start")

			pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(pipelineRun.Spec.Workspaces).To(HaveLen(2))
			registryAuthWorkspace := &v1beta1.WorkspaceBinding{
				Name: "registry-auth",
				Secret: &v1.SecretVolumeSource{
					SecretName: "redhat-appstudio-registry-pull-secret",
				},
			}
			Expect(pipelineRun.Spec.Workspaces).To(ContainElement(*registryAuthWorkspace))
		})

		It("should not be possible to push to quay.io repo (PipelineRun should fail)", func() {
			timeout = time.Minute * 5
			interval = time.Second * 5
			Eventually(func() bool {
				pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, false)
				Expect(err).ShouldNot(HaveOccurred())

				for _, condition := range pipelineRun.Status.Conditions {
					klog.Infof("PipelineRun %s Status.Conditions.Reason: %s\n", pipelineRun.Name, condition.Reason)
					return condition.Reason == "Failed"
				}
				return false
			}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to fail")
		})
	})

	Describe("Creating a component with a specific container image URL", Ordered, func() {

		var applicationName, componentName, testNamespace, outputContainerImage string
		var timeout time.Duration

		BeforeAll(func() {

			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))
			testNamespace = fmt.Sprintf("e2e-test-%s", util.GenerateRandomString(4))

			_, err = f.CommonController.CreateTestNamespace(testNamespace)
			Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", testNamespace, err)

			_, err = f.HasController.CreateHasApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(f.CommonController.DeleteNamespace, testNamespace)
		})

		JustBeforeEach(func() {

			componentName = fmt.Sprintf("build-suite-test-component-image-url-%s", util.GenerateRandomString(4))
			timeout = time.Second * 10

			DeferCleanup(f.HasController.DeleteHasComponent, componentName, testNamespace)
		})
		It("should fail for ContainerImage field set to a protected repository (without an image tag)", func() {
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images-protected", utils.GetQuayIOOrganization())
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(func() ([]string, error) {
				return f.HasController.GetHasComponentConditionStatusMessages(componentName, testNamespace)
			}, timeout).Should(ContainElement(ContainSubstring("create failed")))

		})
		It("should fail for ContainerImage field set to a protected repository followed by a random tag", func() {
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images-protected:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(func() ([]string, error) {
				return f.HasController.GetHasComponentConditionStatusMessages(componentName, testNamespace)
			}, timeout).Should(ContainElement(ContainSubstring("create failed")))
		})
		It("should succeed for ContainerImage field set to a protected repository followed by a namespace prefix + dash + string", func() {
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images-protected:%s-%s", utils.GetQuayIOOrganization(), testNamespace, strings.Replace(uuid.New().String(), "-", "", -1))
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(func() ([]string, error) {
				return f.HasController.GetHasComponentConditionStatusMessages(componentName, testNamespace)
			}, timeout).Should(ContainElement(ContainSubstring("successfully")))
		})
		It("should succeed for ContainerImage field set to a custom (unprotected) repository without a tag being specified", func() {
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images", utils.GetQuayIOOrganization())
			_, err = f.HasController.CreateComponent(applicationName, componentName, testNamespace, helloWorldComponentGitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(func() ([]string, error) {
				return f.HasController.GetHasComponentConditionStatusMessages(componentName, testNamespace)
			}, timeout).Should(ContainElement(ContainSubstring("successfully")))
		})

	})
})
