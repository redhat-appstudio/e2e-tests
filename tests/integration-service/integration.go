package integration

import (
	//	"bytes"
	//	"context"
	"fmt"
	//	"net/http"
	"strings"
	"time"

	"github.com/devfile/library/pkg/util"
	"github.com/google/uuid"
	appservice "github.com/redhat-appstudio/application-service/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"

	//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//appstudioshared "github.com/redhat-appstudio/managed-gitops/appstudio-shared/apis/appstudio.redhat.com/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	klog "k8s.io/klog/v2"
	//	routev1 "github.com/openshift/api/route/v1"
)

const (
	containerImageSource = "quay.io/redhat-appstudio-qe/busybox-loop:latest"
	gitSourceRepoName    = "devfile-sample-python-basic"
	gitSourceURL         = "https://github.com/redhat-appstudio-qe/" + gitSourceRepoName
)

var _ = framework.IntegrationServiceSuiteDescribe("Integration Service E2E tests", Label("integration"), func() {

	defer GinkgoRecover()

	var applicationName, componentName, appStudioE2EApplicationsNamespace, outputContainerImage string
	var pipelineRun v1beta1.PipelineRun
	var component *appservice.Component
	var timeout, interval time.Duration
	//	var applicationSnapshots *[]appstudioshared.ApplicationSnapshot

	var defaultBundleConfigMap *v1.ConfigMap

	// Initialize the tests controllers
	f, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	BeforeAll(func() {
		applicationName = fmt.Sprintf("integration-suite-test-application-%s", util.GenerateRandomString(4))
		appStudioE2EApplicationsNamespace = utils.GetEnv(constants.E2E_APPLICATIONS_NAMESPACE_ENV, "integration-test-namespace")

		_, err := f.CommonController.CreateTestNamespace(appStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", appStudioE2EApplicationsNamespace, err)

		_, err = f.HasController.CreateHasApplication(applicationName, appStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(f.HasController.DeleteHasApplication, applicationName, appStudioE2EApplicationsNamespace)

	})

	When("component with container image source is created", func() {
		BeforeAll(func() {
			componentName = fmt.Sprintf("integration-suite-test-component-image-source-%s", util.GenerateRandomString(4))
			outputContainerImage = ""
			timeout = time.Second * 10
			interval = time.Second * 1
			// Create a component with containerImageSource being defined
			component, err = f.HasController.CreateComponent(applicationName, componentName, appStudioE2EApplicationsNamespace, "", containerImageSource, outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())

			DeferCleanup(f.HasController.DeleteHasComponent, componentName, appStudioE2EApplicationsNamespace)
		})
		It("should not trigger a PipelineRun", func() {
			Consistently(func() bool {
				pipelineRun, err = f.HasController.GetComponentPipelineRun(component.Name, applicationName, appStudioE2EApplicationsNamespace, false)
				Expect(pipelineRun.Name).To(BeEmpty())

				return strings.Contains(err.Error(), "no pipelinerun found")
			}, timeout, interval).Should(BeTrue(), "expected the PipelineRun not to be triggered")
		})
	})
	When("component with git source is created", Label("slow"), func() {

		BeforeAll(func() {

			componentName = fmt.Sprintf("integration-suite-test-component-git-source-%s", util.GenerateRandomString(4))
			outputContainerImage = fmt.Sprintf("quay.io/%s/test-images:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
			timeout = time.Second * 60
			interval = time.Second * 1
			// Create a component with Git Source URL being defined
			component, err = f.HasController.CreateComponent(applicationName, componentName, appStudioE2EApplicationsNamespace, gitSourceURL, "", outputContainerImage, "")
			Expect(err).ShouldNot(HaveOccurred())
			DeferCleanup(f.HasController.DeleteHasComponent, componentName, appStudioE2EApplicationsNamespace)

			defaultBundleConfigMap, err = f.CommonController.GetConfigMap(constants.BuildPipelinesConfigMapName, constants.BuildPipelinesConfigMapDefaultNamespace)
			if err != nil {
				if errors.IsForbidden(err) {
					klog.Infof("don't have enough permissions to get a configmap with default pipeline in %s namespace\n", constants.BuildPipelinesConfigMapDefaultNamespace)
				} else {
					Fail(fmt.Sprintf("error occured when trying to get configmap %s in %s namespace: %v", constants.BuildPipelinesConfigMapName, constants.BuildPipelinesConfigMapDefaultNamespace, err))
				}
			}
			_ = defaultBundleConfigMap.Data["default_build_bundle"]

		})
		It("triggers a PipelineRun", func() {
			Eventually(func() bool {
				pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, appStudioE2EApplicationsNamespace, false)
				if err != nil {
					klog.Infoln("PipelineRun has not been created yet")
					return false
				}
				return pipelineRun.HasStarted()
			}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to start")
		})

		When("the PipelineRun has started", Label("integration-service"), func() {
			BeforeAll(func() {
				timeout = time.Second * 600
				interval = time.Second * 10
			})
			It("should eventually finish successfully", func() {
				Eventually(func() bool {
					pipelineRun, err := f.HasController.GetComponentPipelineRun(componentName, applicationName, appStudioE2EApplicationsNamespace, false)
					Expect(err).ShouldNot(HaveOccurred())

					for _, condition := range pipelineRun.Status.Conditions {
						klog.Infof("PipelineRun %s Status.Conditions.Reason: %s\n", pipelineRun.Name, condition.Reason)

						if condition.Reason == "Failed" {
							Fail(fmt.Sprintf("Pipelinerun %s has failed", pipelineRun.Name))
						}
					}
					return pipelineRun.IsDone()
				}, timeout, interval).Should(BeTrue(), "timed out when waiting for the PipelineRun to finish")
			})
		})

		//	It("check if the ApplicationSnapshot are created",func() {
		//		applicationSnapshots, err = f.IntegrationController.GetAllApplicationSnapshots(applicationName, appStudioE2EApplicationsNamespace)
		//		Expect(err).ShouldNot(HaveOccurred())
		// TBD
		//
		//		 })
		//		It("IntegrationTestScenarios are configed", Label("slow"), func() {
		//			integrationTestScenarios, err := f.IntegrationController.GetIntegrationTestScenarios(applicationName, appStudioE2EApplicationsNamespace)
		//			Expect(err).ShouldNot(HaveOccurred())
		//                       for _, testScenario := range *integrationTestScenarios {
		//      It("check if Integration Service PipelineRun is started"),func() {
		// TBD
		//     })
		//			Expect(f.IntegrationController.WaitForIntegrationPipelineToBeFinished(&testScenario, applicationSnapshots, applicationName, appStudioE2EApplicationsNamespace)).To(Succeed(), "Error when waiting for a integration pipeline to finish")
		//                       }
		//		})
	})
})
