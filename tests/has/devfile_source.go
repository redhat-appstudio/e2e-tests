package has

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devfile/library/pkg/util"
	"github.com/google/uuid"
	appservice "github.com/redhat-appstudio/application-service/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ComponentContainerImage           string = fmt.Sprintf("quay.io/%s/test-images:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
	AppStudioE2EApplicationsNamespace string = utils.GetEnv(constants.E2E_APPLICATIONS_NAMESPACE_ENV, "appstudio-e2e-test")
)

/*
 * Component: application-service
 * Description: Contains tests about creating an application and a quarkus component from a source devfile
 */

var _ = framework.HASSuiteDescribe("[test_id:01] devfile source", Label("has"), func() {
	defer GinkgoRecover()

	var applicationName, componentName string
	// Initialize the tests controllers
	framework, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	// Initialize the application struct
	application := &appservice.Application{}
	cdq := &appservice.ComponentDetectionQuery{}
	compDetected := appservice.ComponentDetectionDescription{}

	BeforeAll(func() {
		applicationName = fmt.Sprintf(RedHatAppStudioApplicationName+"-%s", util.GenerateRandomString(10))
		componentName = fmt.Sprintf(QuarkusComponentName+"-%s", util.GenerateRandomString(10))
		// Check to see if the github token was provided
		Expect(utils.CheckIfEnvironmentExists(constants.GITHUB_TOKEN_ENV)).Should(BeTrue(), "%s environment variable is not set", constants.GITHUB_TOKEN_ENV)
		// Check if 'has-github-token' is present, unless SKIP_HAS_SECRET_CHECK env var is set
		if !utils.CheckIfEnvironmentExists(constants.SKIP_HAS_SECRET_CHECK_ENV) {
			_, err := framework.HasController.KubeInterface().CoreV1().Secrets(RedHatAppStudioApplicationNamespace).Get(context.TODO(), ApplicationServiceGHTokenSecrName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Error checking 'has-github-token' secret %s", err)
		}

		_, err := framework.CommonController.CreateTestNamespace(AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", AppStudioE2EApplicationsNamespace, err)

	})

	AfterAll(func() {

		err = framework.HasController.DeleteHasComponentDetectionQuery(componentName, AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() bool {
			// application info should be stored even after deleting the application in application variable
			gitOpsRepository := utils.ObtainGitOpsRepositoryName(application.Status.Devfile)

			return framework.CommonController.Github.CheckIfRepositoryExist(gitOpsRepository)
		}, 1*time.Minute, 100*time.Millisecond).Should(BeFalse(), "Has controller didn't remove Red Hat AppStudio application gitops repository")

	})

	It("Create Red Hat AppStudio Application", func() {
		createdApplication, err := framework.HasController.CreateHasApplication(applicationName, AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdApplication.Spec.DisplayName).To(Equal(applicationName))
		Expect(createdApplication.Namespace).To(Equal(AppStudioE2EApplicationsNamespace))
	})

	It("Check Red Hat AppStudio Application health", func() {
		Eventually(func() string {
			application, err = framework.HasController.GetHasApplication(applicationName, AppStudioE2EApplicationsNamespace)
			Expect(err).NotTo(HaveOccurred())

			return application.Status.Devfile
		}, 3*time.Minute, 100*time.Millisecond).Should(Not(BeEmpty()), "Error creating gitOps repository")

		Eventually(func() bool {
			// application info should be stored even after deleting the application in application variable
			gitOpsRepository := utils.ObtainGitOpsRepositoryName(application.Status.Devfile)

			return framework.CommonController.Github.CheckIfRepositoryExist(gitOpsRepository)
		}, 1*time.Minute, 1*time.Second).Should(BeTrue(), "Has controller didn't create gitops repository")
	})

	// Necessary for component pipeline
	It("Check if 'git-clone' cluster tasks exists", func() {
		Eventually(func() bool {
			return framework.CommonController.CheckIfClusterTaskExists("git-clone")
		}, 5*time.Minute, 45*time.Second).Should(BeTrue(), "'git-clone' cluster task don't exist in cluster. Component cannot be created")
	})

	It("Create Red Hat AppStudio ComponentDetectionQuery for Component repository", func() {
		cdq, err := framework.HasController.CreateComponentDetectionQuery(componentName, AppStudioE2EApplicationsNamespace, QuarkusDevfileSource, "", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(cdq.Name).To(Equal(componentName))

	})

	It("Check Red Hat AppStudio ComponentDetectionQuery status", func() {
		// Validate that the CDQ completes successfully
		Eventually(func() bool {
			// application info should be stored even after deleting the application in application variable
			cdq, err = framework.HasController.GetComponentDetectionQuery(componentName, AppStudioE2EApplicationsNamespace)
			return err == nil && len(cdq.Status.ComponentDetected) > 0
		}, 1*time.Minute, 1*time.Second).Should(BeTrue(), "ComponentDetectionQuery did not complete successfully")

		// Validate that the completed CDQ only has one detected component
		Expect(len(cdq.Status.ComponentDetected)).To(Equal(1), "Expected length of the detected Components was not 1")

		// Get the stub CDQ and validate its content
		for _, compDetected = range cdq.Status.ComponentDetected {
			Expect(compDetected.DevfileFound).To(BeTrue(), "DevfileFound was not set to true")
			Expect(compDetected.Language).To(Equal("java"), "Detected language was not java")
			Expect(compDetected.ProjectType).To(Equal("quarkus"), "Detected framework was not quarkus")
		}
	})

	It("Create Red Hat AppStudio Quarkus component", func() {
		component, err := framework.HasController.CreateComponentFromStub(compDetected, componentName, AppStudioE2EApplicationsNamespace, "", applicationName)
		Expect(err).NotTo(HaveOccurred())
		Expect(component.Name).To(Equal(componentName))
	})

	It("Check a Component gets deleted when its application is deleted", func() {
		err = framework.HasController.DeleteHasApplication(applicationName, AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			_, err := framework.HasController.GetHasComponent(componentName, AppStudioE2EApplicationsNamespace)
			if err != nil && errors.IsNotFound(err) {
				return true
			}

			return false
		}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "Component didn't get get deleted with its Application")
		Expect(err).NotTo(HaveOccurred())
	})
})
