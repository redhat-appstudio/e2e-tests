package integration

import (
	"fmt"
	"strings"
	"time"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	integrationv1beta1 "github.com/konflux-ci/integration-service/api/v1beta1"
	intgteststat "github.com/konflux-ci/integration-service/pkg/integrationteststatus"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = framework.IntegrationServiceSuiteDescribe("Integration Service E2E tests", Label("integration-service", "integration-env"), func() {
	defer GinkgoRecover()

	var f *framework.Framework
	var err error

	var applicationName, componentName, testNamespace string
	var integrationTestScenario *integrationv1beta1.IntegrationTestScenario
	var timeout, interval time.Duration
	var originalComponent *appstudioApi.Component
	var pipelineRun, integrationPipelineRun *pipeline.PipelineRun
	var snapshot *appstudioApi.Snapshot
	var spaceRequest *v1alpha1.SpaceRequest

	AfterEach(framework.ReportFailure(&f))

	Describe("with happy path for general flow of Integration service with ephemeral environment", Ordered, func() {
		BeforeAll(func() {
			// Initialize the tests controllers
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("integration-env"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			applicationName = createApp(*f, testNamespace)
			componentName, originalComponent = createComponent(*f, testNamespace, applicationName)
			integrationTestScenario, err = f.AsKubeAdmin.IntegrationController.CreateIntegrationTestScenario("", applicationName, testNamespace, gitURL, revision, pathIntegrationPipelineWithEnv)
			Expect(err).ShouldNot(HaveOccurred())
		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}
		})

		When("a new Component is created", func() {
			It("triggers a build PipelineRun", Label("integration-service"), func() {
				pipelineRun, err = f.AsKubeDeveloper.IntegrationController.GetBuildPipelineRun(componentName, applicationName, testNamespace, false, "")
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("verifies if the build PipelineRun contains the finalizer", Label("integration-service"), func() {
				Eventually(func() error {
					pipelineRun, err = f.AsKubeDeveloper.IntegrationController.GetBuildPipelineRun(componentName, applicationName, testNamespace, false, "")
					Expect(err).ShouldNot(HaveOccurred())
					if !controllerutil.ContainsFinalizer(pipelineRun, pipelinerunFinalizerByIntegrationService) {
						return fmt.Errorf("build pipelineRun %s/%s doesn't contain the finalizer: %s yet", pipelineRun.GetNamespace(), pipelineRun.GetName(), pipelinerunFinalizerByIntegrationService)
					}
					return nil
				}, 1*time.Minute, 1*time.Second).Should(Succeed(), "timeout when waiting for finalizer to be added")
			})

			It("waits for build PipelineRun to succeed", Label("integration-service"), func() {
				Expect(pipelineRun.Annotations[snapshotAnnotation]).To(Equal(""))
				Expect(f.AsKubeDeveloper.HasController.WaitForComponentPipelineToBeFinished(originalComponent, "",
					f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, pipelineRun)).To(Succeed())
			})
		})

		When("the build pipelineRun run succeeded", func() {
			It("checks if the BuildPipelineRun have the annotation of chains signed", func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForBuildPipelineRunToGetAnnotated(testNamespace, applicationName, componentName, chainsSignedAnnotation)).To(Succeed())
			})

			It("checks if the Snapshot is created", func() {
				snapshot, err = f.AsKubeDeveloper.IntegrationController.WaitForSnapshotToGetCreated("", "", componentName, testNamespace)

			})

			It("checks if the Build PipelineRun got annotated with Snapshot name", func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForBuildPipelineRunToGetAnnotated(testNamespace, applicationName, componentName, snapshotAnnotation)).To(Succeed())
			})

			It("verifies that the finalizer has been removed from the build pipelinerun", func() {
				timeout := "60s"
				interval := "1s"
				Eventually(func() error {
					pipelineRun, err = f.AsKubeDeveloper.IntegrationController.GetBuildPipelineRun(componentName, applicationName, testNamespace, false, "")
					Expect(err).ShouldNot(HaveOccurred())
					if controllerutil.ContainsFinalizer(pipelineRun, pipelinerunFinalizerByIntegrationService) {
						return fmt.Errorf("build pipelineRun %s/%s still contains the finalizer: %s", pipelineRun.GetNamespace(), pipelineRun.GetName(), pipelinerunFinalizerByIntegrationService)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "timeout when waiting for finalizer to be removed")
			})

			It(fmt.Sprintf("checks if CronJob %s exists", spaceRequestCronJobName), func() {
				spaceRequestCleanerCronJob, err := f.AsKubeAdmin.CommonController.GetCronJob(spaceRequestCronJobNamespace, spaceRequestCronJobName)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(strings.Contains(spaceRequestCleanerCronJob.Name, spaceRequestCronJobName)).Should(BeTrue())
			})

			It("checks if all of the integrationPipelineRuns passed", Label("slow"), func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForAllIntegrationPipelinesToBeFinished(testNamespace, applicationName, snapshot)).To(Succeed())
			})

			It("checks if space request is created in namespace", func() {
				spaceRequestsList, err := f.AsKubeAdmin.GitOpsController.GetSpaceRequests(testNamespace)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(spaceRequestsList.Items).ToNot(BeEmpty(), "Expected spaceRequestsList.Items to have at least one item")
				spaceRequest = &spaceRequestsList.Items[0]
				Expect(strings.Contains(spaceRequest.Name, spaceRequestNamePrefix)).Should(BeTrue())
			})

			It("checks if the passed status of integration test is reported in the Snapshot", func() {
				timeout = time.Second * 240
				interval = time.Second * 5
				Eventually(func() error {
					snapshot, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshot.Name, "", "", testNamespace)
					Expect(err).ShouldNot(HaveOccurred())

					statusDetail, err := f.AsKubeDeveloper.IntegrationController.GetIntegrationTestStatusDetailFromSnapshot(snapshot, integrationTestScenario.Name)
					Expect(err).ToNot(HaveOccurred())

					if statusDetail.Status != intgteststat.IntegrationTestStatusTestPassed {
						return fmt.Errorf("test status for scenario: %s, doesn't have expected value %s, within the snapshot: %s", integrationTestScenario.Name, intgteststat.IntegrationTestStatusTestPassed, snapshot.Name)
					}
					return nil
				}, timeout, interval).Should(Succeed())
			})

			It("checks if the finalizer was removed from all of the related Integration pipelineRuns", func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForFinalizerToGetRemovedFromAllIntegrationPipelineRuns(testNamespace, applicationName, snapshot)).To(Succeed())
			})

			It("checks that when deleting integration test scenario pipelineRun, spaceRequest is deleted too", func() {
				integrationPipelineRun, err = f.AsKubeAdmin.IntegrationController.GetIntegrationPipelineRun(integrationTestScenario.Name, snapshot.Name, testNamespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(f.AsKubeDeveloper.TektonController.DeletePipelineRun(integrationPipelineRun.Name, integrationPipelineRun.Namespace)).To(Succeed())

				timeout = time.Second * 200
				interval = time.Second * 5
				Eventually(func() error {
					currentSpaceRequest, err := f.AsKubeAdmin.GitOpsController.GetSpaceRequest(testNamespace, spaceRequest.Name)
					if err != nil {
						if k8sErrors.IsNotFound(err) {
							return nil
						}
						return fmt.Errorf("failed to get %s/%s spaceRequest: %+v", currentSpaceRequest.Namespace, currentSpaceRequest.Name, err)
					}
					return fmt.Errorf("spaceRequest %s/%s still exists", currentSpaceRequest.Namespace, currentSpaceRequest.Name)
				}, timeout, interval).Should(Succeed())

			})
		})
	})
})
