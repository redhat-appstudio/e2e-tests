package integration

import (
	"fmt"
	"time"

	"github.com/konflux-ci/operator-toolkit/metadata"

	"github.com/devfile/library/v2/pkg/util"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/has"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	integrationv1beta1 "github.com/redhat-appstudio/integration-service/api/v1beta1"
	intgteststat "github.com/redhat-appstudio/integration-service/pkg/integrationteststatus"

	appstudioApi "github.com/redhat-appstudio/application-api/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = framework.IntegrationServiceSuiteDescribe("Integration Service E2E tests", Label("integration-service", "HACBS"), func() {
	defer GinkgoRecover()

	var f *framework.Framework
	var err error

	var applicationName, componentName, testNamespace string
	var integrationTestScenario *integrationv1beta1.IntegrationTestScenario
	var newIntegrationTestScenario *integrationv1beta1.IntegrationTestScenario
	var timeout, interval time.Duration
	var originalComponent *appstudioApi.Component
	var pipelineRun *pipeline.PipelineRun
	var snapshot *appstudioApi.Snapshot
	var snapshotPush *appstudioApi.Snapshot
	var env *appstudioApi.Environment
	AfterEach(framework.ReportFailure(&f))

	Describe("with happy path for general flow of Integration service", Ordered, func() {
		BeforeAll(func() {
			// Initialize the tests controllers
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("integration1"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			applicationName = createApp(*f, testNamespace)
			componentName, originalComponent = createComponent(*f, testNamespace, applicationName)
			integrationTestScenario, err = f.AsKubeAdmin.IntegrationController.CreateIntegrationTestScenario(applicationName, testNamespace, gitURL, revision, pathInRepoPass)
			Expect(err).ShouldNot(HaveOccurred())
		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				cleanup(*f, testNamespace, applicationName, componentName)

				Expect(f.AsKubeAdmin.IntegrationController.DeleteSnapshot(snapshotPush, testNamespace)).To(Succeed())
				Expect(f.AsKubeAdmin.GitOpsController.DeleteAllEnvironmentsInASpecificNamespace(EnvironmentName, time.Minute*5)).To(Succeed())
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
				Expect(f.AsKubeDeveloper.HasController.WaitForComponentPipelineToBeFinished(originalComponent, "",
					f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
				Expect(pipelineRun.Annotations[snapshotAnnotation]).To(Equal(""))
			})
		})

		When("the build pipelineRun run succeeded", func() {
			It("checks if the BuildPipelineRun have the annotation of chains signed", func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForBuildPipelineRunToGetAnnotated(testNamespace, applicationName, componentName, chainsSignedAnnotation)).To(Succeed())
			})

			It("checks if the Snapshot is created", func() {
				snapshot, err = f.AsKubeDeveloper.IntegrationController.WaitForSnapshotToGetCreated("", "", componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
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

			It("checks if all of the integrationPipelineRuns passed", Label("slow"), func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForAllIntegrationPipelinesToBeFinished(testNamespace, applicationName, snapshot)).To(Succeed())
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
		})

		It("creates a ReleasePlan and an environment", func() {
			_, err = f.AsKubeAdmin.ReleaseController.CreateReleasePlan(autoReleasePlan, testNamespace, applicationName, targetReleaseNamespace, "")
			Expect(err).ShouldNot(HaveOccurred())
			env, err = f.AsKubeAdmin.GitOpsController.CreatePocEnvironment(EnvironmentName, testNamespace)
			Expect(err).ShouldNot(HaveOccurred())
			testScenarios, err := f.AsKubeAdmin.IntegrationController.GetIntegrationTestScenarios(applicationName, testNamespace)
			Expect(err).ShouldNot(HaveOccurred())
			for _, testScenario := range *testScenarios {
				GinkgoWriter.Printf("IntegrationTestScenario %s is found\n", testScenario.Name)
			}
		})

		It("creates an snapshot of push event", func() {
			sampleImage := "quay.io/redhat-appstudio/sample-image@sha256:841328df1b9f8c4087adbdcfec6cc99ac8308805dea83f6d415d6fb8d40227c1"
			snapshotPush, err = f.AsKubeAdmin.IntegrationController.CreateSnapshotWithImage(componentName, applicationName, testNamespace, sampleImage)
			Expect(err).ShouldNot(HaveOccurred())
		})

		When("An snapshot of push event is created", func() {
			It("checks if all of the integrationPipelineRuns created by push event passed", Label("slow"), func() {
				Expect(f.AsKubeAdmin.IntegrationController.WaitForAllIntegrationPipelinesToBeFinished(testNamespace, applicationName, snapshotPush)).To(Succeed(), "Error when waiting for one of the integration pipelines to finish in %s namespace", testNamespace)
			})

			It("checks if the global candidate is updated after push event", func() {
				timeout = time.Second * 600
				interval = time.Second * 10
				Eventually(func() error {
					snapshotPush, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshotPush.Name, "", "", testNamespace)
					Expect(err).ShouldNot(HaveOccurred())

					if f.AsKubeAdmin.CommonController.HaveTestsSucceeded(snapshotPush) {
						component, err := f.AsKubeAdmin.HasController.GetComponentByApplicationName(applicationName, testNamespace)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(component.Spec.ContainerImage).ToNot(Equal(originalComponent.Spec.ContainerImage))
						return nil
					}
					return fmt.Errorf("tests haven't succeeded yet for snapshot %s/%s", snapshotPush.GetNamespace(), snapshotPush.GetName())
				}, timeout, interval).Should(Succeed(), fmt.Sprintf("time out when waiting for updating the global candidate in %s namespace", testNamespace))
			})

			It("checks if a Release is created successfully", func() {
				timeout = time.Second * 60
				interval = time.Second * 5
				Eventually(func() error {
					_, err := f.AsKubeAdmin.ReleaseController.GetReleases(testNamespace)
					return err
				}, timeout, interval).Should(Succeed(), fmt.Sprintf("time out when waiting for release created for snapshot %s/%s", snapshotPush.GetNamespace(), snapshotPush.GetName()))
			})

			It("checks if an SnapshotEnvironmentBinding is created successfully", func() {
				timeout = time.Second * 600
				interval = time.Second * 2
				Eventually(func() error {
					_, err := f.AsKubeAdmin.CommonController.GetSnapshotEnvironmentBinding(applicationName, testNamespace, env)
					return err
				}, timeout, interval).Should(Succeed(), fmt.Sprintf("timed out when waiting for SnapshotEnvironmentBinding to be created for application %s/%s", testNamespace, applicationName))
			})
		})
	})

	Describe("with an integration test fail", Ordered, func() {
		BeforeAll(func() {
			// Initialize the tests controllers
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("integration2"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			applicationName = createApp(*f, testNamespace)
			componentName, originalComponent = createComponent(*f, testNamespace, applicationName)

			env, err = f.AsKubeAdmin.GitOpsController.CreatePocEnvironment(EnvironmentName, testNamespace)
			Expect(err).ShouldNot(HaveOccurred())
			integrationTestScenario, err = f.AsKubeAdmin.IntegrationController.CreateIntegrationTestScenario(applicationName, testNamespace, gitURL, revision, pathInRepoFail)
			Expect(err).ShouldNot(HaveOccurred())
		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				cleanup(*f, testNamespace, applicationName, componentName)

				Expect(f.AsKubeAdmin.GitOpsController.DeleteAllEnvironmentsInASpecificNamespace(EnvironmentName, time.Minute*5)).To(Succeed())
			}
		})

		It("triggers a build PipelineRun", Label("integration-service"), func() {
			pipelineRun, err = f.AsKubeDeveloper.IntegrationController.GetBuildPipelineRun(componentName, applicationName, testNamespace, false, "")
			Expect(f.AsKubeDeveloper.HasController.WaitForComponentPipelineToBeFinished(originalComponent, "", f.AsKubeAdmin.TektonController,
				&has.RetryOptions{Retries: 2, Always: true})).To(Succeed())

			Expect(pipelineRun.Annotations[snapshotAnnotation]).To(Equal(""))
		})

		It("checks if the BuildPipelineRun have the annotation of chains signed", func() {
			Expect(f.AsKubeDeveloper.IntegrationController.WaitForBuildPipelineRunToGetAnnotated(testNamespace, applicationName, componentName, chainsSignedAnnotation)).To(Succeed())
		})

		It("checks if the Snapshot is created", func() {
			snapshot, err = f.AsKubeDeveloper.IntegrationController.WaitForSnapshotToGetCreated("", pipelineRun.Name, componentName, testNamespace)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("checks if the Build PipelineRun got annotated with Snapshot name", func() {
			Expect(f.AsKubeDeveloper.IntegrationController.WaitForBuildPipelineRunToGetAnnotated(testNamespace, applicationName, componentName, snapshotAnnotation)).To(Succeed())
		})

		It("checks if all of the integrationPipelineRuns finished", Label("slow"), func() {
			Expect(f.AsKubeDeveloper.IntegrationController.WaitForAllIntegrationPipelinesToBeFinished(testNamespace, applicationName, snapshot)).To(Succeed())
		})

		It("checks if the failed status of integration test is reported in the Snapshot", func() {
			Eventually(func() error {
				snapshot, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshot.Name, "", "", testNamespace)
				Expect(err).ShouldNot(HaveOccurred())

				statusDetail, err := f.AsKubeDeveloper.IntegrationController.GetIntegrationTestStatusDetailFromSnapshot(snapshot, integrationTestScenario.Name)
				Expect(err).ToNot(HaveOccurred())

				if statusDetail.Status != intgteststat.IntegrationTestStatusTestFail {
					return fmt.Errorf("test status doesn't have expected value %s", intgteststat.IntegrationTestStatusTestFail)
				}
				return nil
			}, timeout, interval).Should(Succeed())
		})

		It("checks if snapshot is marked as failed", FlakeAttempts(3), func() {
			snapshot, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshot.Name, "", "", testNamespace)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(f.AsKubeAdmin.CommonController.HaveTestsSucceeded(snapshot)).To(BeFalse(), "expected tests to fail for snapshot %s/%s", snapshot.GetNamespace(), snapshot.GetName())
		})

		It("checks if the finalizer was removed from all of the related Integration pipelineRuns", func() {
			Expect(f.AsKubeDeveloper.IntegrationController.WaitForFinalizerToGetRemovedFromAllIntegrationPipelineRuns(testNamespace, applicationName, snapshot)).To(Succeed())
		})

		It("creates a new IntegrationTestScenario", func() {
			newIntegrationTestScenario, err = f.AsKubeAdmin.IntegrationController.CreateIntegrationTestScenario(applicationName, testNamespace, gitURL, revision, pathInRepoPass)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("updates the Snapshot with the re-run label for the new scenario", FlakeAttempts(3), func() {
			updatedSnapshot := snapshot.DeepCopy()
			err := metadata.AddLabels(updatedSnapshot, map[string]string{snapshotRerunLabel: newIntegrationTestScenario.Name})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(f.AsKubeAdmin.IntegrationController.PatchSnapshot(snapshot, updatedSnapshot)).Should(Succeed())
			Expect(metadata.GetLabelsWithPrefix(updatedSnapshot, snapshotRerunLabel)).NotTo(BeEmpty())
		})

		When("An snapshot is updated with a re-run label for a given scenario", func() {
			It("checks if the new integration pipelineRun started", Label("slow"), func() {
				reRunPipelineRun, err := f.AsKubeDeveloper.IntegrationController.WaitForIntegrationPipelineToGetStarted(newIntegrationTestScenario.Name, snapshot.Name, testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(reRunPipelineRun).ShouldNot(BeNil())
			})

			It("checks if the re-run label was removed from the Snapshot", func() {
				Eventually(func() error {
					snapshot, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshot.Name, "", "", testNamespace)
					if err != nil {
						return fmt.Errorf("encountered error while getting Snapshot %s/%s: %w", snapshot.Name, snapshot.Namespace, err)
					}

					if metadata.HasLabel(snapshot, snapshotRerunLabel) {
						return fmt.Errorf("the Snapshot %s/%s shouldn't contain the %s label", snapshot.Name, snapshot.Namespace, snapshotRerunLabel)
					}
					return nil
				}, timeout, interval).Should(Succeed())
			})

			It("checks if all integration pipelineRuns finished successfully", Label("slow"), func() {
				Expect(f.AsKubeDeveloper.IntegrationController.WaitForAllIntegrationPipelinesToBeFinished(testNamespace, applicationName, snapshot)).To(Succeed())
			})

			It("checks if the name of the re-triggered pipelinerun is reported in the Snapshot", FlakeAttempts(3), func() {
				Eventually(func(g Gomega) {
					snapshot, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshot.Name, "", "", testNamespace)
					g.Expect(err).ShouldNot(HaveOccurred())

					statusDetail, err := f.AsKubeDeveloper.IntegrationController.GetIntegrationTestStatusDetailFromSnapshot(snapshot, newIntegrationTestScenario.Name)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(statusDetail).NotTo(BeNil())

					integrationPipelineRun, err := f.AsKubeDeveloper.IntegrationController.GetIntegrationPipelineRun(newIntegrationTestScenario.Name, snapshot.Name, testNamespace)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(integrationPipelineRun).NotTo(BeNil())

					g.Expect(statusDetail.TestPipelineRunName).To(Equal(integrationPipelineRun.Name))
				}, timeout, interval).Should(Succeed())
			})

			It("checks if snapshot is still marked as failed", func() {
				snapshot, err = f.AsKubeAdmin.IntegrationController.GetSnapshot(snapshot.Name, "", "", testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(f.AsKubeAdmin.CommonController.HaveTestsSucceeded(snapshot)).To(BeFalse(), "expected tests to fail for snapshot %s/%s", snapshot.GetNamespace(), snapshot.GetName())
			})

		})

		It("creates an snapshot of push event", func() {
			sampleImage := "quay.io/redhat-appstudio/sample-image@sha256:841328df1b9f8c4087adbdcfec6cc99ac8308805dea83f6d415d6fb8d40227c1"
			snapshotPush, err = f.AsKubeAdmin.IntegrationController.CreateSnapshotWithImage(componentName, applicationName, testNamespace, sampleImage)
			Expect(err).ShouldNot(HaveOccurred())
		})

		When("An snapshot of push event is created", func() {
			It("checks no Release CRs are created", func() {
				releases, err := f.AsKubeAdmin.ReleaseController.GetReleases(testNamespace)
				Expect(err).NotTo(HaveOccurred(), "Error when fetching the Releases")
				Expect(releases.Items).To(BeEmpty(), "Expected no Release CRs to be present, but found some")
			})

			It("checks no SnapshotEnvironmentBinding is created", func() {
				seb, err := f.AsKubeAdmin.CommonController.GetSnapshotEnvironmentBinding(applicationName, testNamespace, env)

				if err != nil {
					Expect(err.Error()).To(ContainSubstring("no SnapshotEnvironmentBinding found"))
				} else {
					Expect(seb).To(BeNil(), "Expected no SnapshotEnvironmentBinding to be present, but found one")
				}
			})

			It("checks if the global candidate is not updated", func() {
				// give some time to do eventual updates in component
				time.Sleep(60 * time.Second)

				component, err := f.AsKubeAdmin.HasController.GetComponentByApplicationName(applicationName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred())

				// global candidate is not updated
				Expect(component.Spec.ContainerImage).To(Equal(originalComponent.Spec.ContainerImage))
			})
		})
	})
})

func createApp(f framework.Framework, testNamespace string) string {
	applicationName := fmt.Sprintf("integ-app-%s", util.GenerateRandomString(4))

	app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
	Expect(err).NotTo(HaveOccurred())
	Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
		Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
	)

	return applicationName
}

func createComponent(f framework.Framework, testNamespace, applicationName string) (string, *appstudioApi.Component) {
	var originalComponent *appstudioApi.Component

	componentName := fmt.Sprintf("integration-suite-test-component-git-source-%s", util.GenerateRandomString(6))
	// Create a component with Git Source URL being defined
	// using cdq since git ref is not known
	cdq, err := f.AsKubeAdmin.HasController.CreateComponentDetectionQuery(componentName, testNamespace, componentRepoURL, "", "", "", false)
	Expect(err).NotTo(HaveOccurred())
	Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

	for _, compDetected := range cdq.Status.ComponentDetected {
		originalComponent, err = f.AsKubeAdmin.HasController.CreateComponent(compDetected.ComponentStub, testNamespace, "", "", applicationName, true, map[string]string{})
		Expect(err).NotTo(HaveOccurred())
		Expect(originalComponent).NotTo(BeNil())
		componentName = originalComponent.Name
	}

	return componentName, originalComponent
}

func cleanup(f framework.Framework, testNamespace, applicationName, componentName string) {
	if !CurrentSpecReport().Failed() {
		Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
		Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
		integrationTestScenarios, err := f.AsKubeAdmin.IntegrationController.GetIntegrationTestScenarios(applicationName, testNamespace)
		Expect(err).ShouldNot(HaveOccurred())

		for _, testScenario := range *integrationTestScenarios {
			Expect(f.AsKubeAdmin.IntegrationController.DeleteIntegrationTestScenario(&testScenario, testNamespace)).To(Succeed())
		}
		Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
	}
}
