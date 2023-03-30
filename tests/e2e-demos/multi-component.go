package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/e2e-tests/tests/e2e-demos/config"
	v1 "k8s.io/api/core/v1"
)

const (
	multiComponentTestNamespace string = "multi-comp-e2e"
)

var _ = framework.E2ESuiteDescribe(Label("e2e-demo", "multi-component"), func() {
	defer GinkgoRecover()

	var fw *framework.Framework
	var err error
	var namespace string

	// Initialize the application struct
	application := &appservice.Application{}
	cdq := &appservice.ComponentDetectionQuery{}
	componentList := []*appservice.Component{}
	env := &appservice.Environment{}

	var testSpecification = config.WorkflowSpec{
		Tests: []config.TestSpec{
			{
				Name:            "multi-component-application",
				ApplicationName: "multi-component-application",
				Components: []config.ComponentSpec{
					{
						Name:         "mc-withdockerfile-withoutdevfile",
						Type:         "public",
						GitSourceUrl: "https://github.com/redhat-appstudio/quality-dashboard.git",
					},
				},
			},
		},
	}

	for _, suite := range testSpecification.Tests {
		Describe(suite.Name, Ordered, func() {
			BeforeAll(func() {
				if suite.Skip {
					Skip(fmt.Sprintf("test skipped %s", suite.Name))
				}

				// Initialize the tests controllers
				fw, err = framework.NewFramework(utils.GetGeneratedNamespace(multiComponentTestNamespace))
				Expect(err).NotTo(HaveOccurred())
				namespace = fw.UserNamespace
				Expect(namespace).NotTo(BeEmpty())

				suiteConfig, _ := GinkgoConfiguration()
				GinkgoWriter.Printf("Parallel processes: %d\n", suiteConfig.ParallelTotal)
				GinkgoWriter.Printf("Running on namespace: %s\n", namespace)
				GinkgoWriter.Printf("User: %s\n", fw.UserName)

				githubCredentials := `{"access_token":"` + utils.GetEnv(constants.GITHUB_TOKEN_ENV, "") + `"}`

				_ = fw.AsKubeDeveloper.SPIController.InjectManualSPIToken(namespace, fmt.Sprintf("https://github.com/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")), githubCredentials, v1.SecretTypeBasicAuth, SPIGithubSecretName)

			})

			// Remove all resources created by the tests
			AfterAll(func() {
				if !CurrentSpecReport().Failed() {
					Expect(fw.AsKubeDeveloper.HasController.DeleteAllComponentsInASpecificNamespace(namespace, 30*time.Second)).To(Succeed())
					Expect(fw.AsKubeAdmin.HasController.DeleteAllApplicationsInASpecificNamespace(namespace, 30*time.Second)).To(Succeed())
					Expect(fw.AsKubeAdmin.HasController.DeleteAllSnapshotEnvBindingsInASpecificNamespace(namespace, 30*time.Second)).To(Succeed())
					Expect(fw.AsKubeAdmin.ReleaseController.DeleteAllSnapshotsInASpecificNamespace(namespace, 30*time.Second)).To(Succeed())
					Expect(fw.AsKubeAdmin.GitOpsController.DeleteAllEnvironmentsInASpecificNamespace(namespace, 30*time.Second)).To(Succeed())
					Expect(fw.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(namespace)).To(Succeed())
					Expect(fw.AsKubeAdmin.GitOpsController.DeleteAllGitOpsDeploymentInASpecificNamespace(namespace, 30*time.Second)).To(Succeed())
					Expect(fw.SandboxController.DeleteUserSignup(fw.UserName)).NotTo(BeFalse())
				}
			})

			// Create an application in a specific namespace
			It(fmt.Sprintf("create application %s", suite.ApplicationName), func() {
				GinkgoWriter.Printf("Parallel process %d\n", GinkgoParallelProcess())
				application, err := fw.AsKubeDeveloper.HasController.CreateHasApplication(suite.ApplicationName, namespace)
				Expect(err).NotTo(HaveOccurred())
				Expect(application.Spec.DisplayName).To(Equal(suite.ApplicationName))
				Expect(application.Namespace).To(Equal(namespace))
			})

			// Check the application health and check if a devfile was generated in the status
			It(fmt.Sprintf("checks if application %s is healthy", suite.ApplicationName), func() {
				Eventually(func() string {
					appstudioApp, err := fw.AsKubeDeveloper.HasController.GetHasApplication(suite.ApplicationName, namespace)
					Expect(err).NotTo(HaveOccurred())
					application = appstudioApp

					return application.Status.Devfile
				}, 3*time.Minute, 100*time.Millisecond).Should(Not(BeEmpty()), "Error creating gitOps repository")

				Eventually(func() bool {
					gitOpsRepository := utils.ObtainGitOpsRepositoryName(application.Status.Devfile)

					return fw.AsKubeDeveloper.CommonController.Github.CheckIfRepositoryExist(gitOpsRepository)
				}, 5*time.Minute, 1*time.Second).Should(BeTrue(), "Has controller didn't create gitops repository")
			})

			It(fmt.Sprintf("creates ComponentDetectionQuery for application %s", suite.ApplicationName), func() {
				cdq, err = fw.AsKubeDeveloper.HasController.CreateComponentDetectionQuery(
					suite.Components[0].Name,
					namespace,
					suite.Components[0].GitSourceUrl,
					suite.Components[0].GitSourceRevision,
					suite.Components[0].GitSourceContext,
					"",
					false,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(cdq.Name).To(Equal(suite.Components[0].Name))
			})

			// Create an environment in a specific namespace
			It(fmt.Sprintf("creates environment %s", EnvironmentName), func() {
				env, err = fw.AsKubeDeveloper.IntegrationController.CreateEnvironment(namespace, EnvironmentName)
				Expect(err).NotTo(HaveOccurred())
			})

			It(fmt.Sprintf("creates multiple components in application %s", suite.ApplicationName), func() {
				for _, component := range cdq.Status.ComponentDetected {
					c, err := fw.AsKubeDeveloper.HasController.CreateComponentFromStub(component, component.ComponentStub.ComponentName, namespace, SPIGithubSecretName, application.Name, "")
					Expect(err).NotTo(HaveOccurred())
					Expect(c.Name).To(Equal(component.ComponentStub.ComponentName))
					componentList = append(componentList, c)
				}
			})

			It(fmt.Sprintf("waits application %s components pipelines to be finished", application.Name), func() {
				GinkgoWriter.Print(env)
				for _, component := range componentList {
					if err := fw.AsKubeDeveloper.HasController.WaitForComponentPipelineToBeFinished(fw.AsKubeAdmin.CommonController, component.Name, suite.ApplicationName, namespace, ""); err != nil {
						Fail(fmt.Sprint(err))
					}
				}
			})

			It(fmt.Sprintf("finds the application %s components snapshots and checks if it is marked as successfully", suite.ApplicationName), func() {
				timeout := time.Second * 600
				interval := time.Second * 10

				for _, component := range componentList {
					componentSnapshot, err := fw.AsKubeDeveloper.IntegrationController.GetApplicationSnapshot("", application.Name, namespace, component.Name)
					Expect(err).ShouldNot(HaveOccurred())

					Eventually(func() bool {
						return fw.AsKubeDeveloper.IntegrationController.HaveHACBSTestsSucceeded(componentSnapshot)
					}, timeout, interval).Should(BeTrue(), "time out when trying to check if the snapshot is marked as successful")
				}
			})
		})
	}
})
