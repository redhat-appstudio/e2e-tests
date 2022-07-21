package has

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/devfile/library/pkg/util"
	"github.com/google/uuid"
	appservice "github.com/redhat-appstudio/application-service/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/service-provider-integration-operator/api/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	PrivateComponentContainerImage string = fmt.Sprintf("quay.io/%s/quarkus:%s", utils.GetQuayIOOrganization(), strings.Replace(uuid.New().String(), "-", "", -1))
)

/*
 * Component: application-service
 * Description: Contains tests about creating an application and a quarkus component from a source devfile
 */

var _ = framework.HASSuiteDescribe("[test_id:02] private devfile source", Label("has"), func() {
	defer GinkgoRecover()

	// Initialize the tests controllers
	framework, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	var applicationName, componentName string

	// Initialize the application struct
	application := &appservice.Application{}
	cdq := &appservice.ComponentDetectionQuery{}
	compDetected := appservice.ComponentDetectionDescription{}
	privateGitRepository := utils.GetEnv(constants.PRIVATE_DEVFILE_SAMPLE, PrivateQuarkusDevfileSource)

	BeforeAll(func() {
		// Generate names for the application and component resources
		applicationName = fmt.Sprintf(RedHatAppStudioApplicationName+"-%s", util.GenerateRandomString(10))
		componentName = fmt.Sprintf(QuarkusComponentName+"-%s", util.GenerateRandomString(10))

		_, err = framework.CommonController.CreateTestNamespace(AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", AppStudioE2EApplicationsNamespace, err)

		createAndInjectTokenToSPI(framework, utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""), privateGitRepository, SPIAccessTokenBindingName, AppStudioE2EApplicationsNamespace, SPIAccessTokenSecretName)

		// Check to see if the github token was provided
		Expect(utils.CheckIfEnvironmentExists(constants.GITHUB_TOKEN_ENV)).Should(BeTrue(), "%s environment variable is not set", constants.GITHUB_TOKEN_ENV)
		// Check if 'has-github-token' is present, unless SKIP_HAS_SECRET_CHECK env var is set
		if !utils.CheckIfEnvironmentExists(constants.SKIP_HAS_SECRET_CHECK_ENV) {
			_, err := framework.HasController.KubeInterface().CoreV1().Secrets(RedHatAppStudioApplicationNamespace).Get(context.TODO(), ApplicationServiceGHTokenSecrName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Error checking 'has-github-token' secret %s", err)
		}

	})

	AfterAll(func() {
		err := framework.HasController.DeleteHasComponent(componentName, AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred())

		err = framework.HasController.DeleteHasApplication(applicationName, AppStudioE2EApplicationsNamespace)
		Expect(err).NotTo(HaveOccurred())

		err = framework.SPIController.DeleteSPIAccessTokenBinding(SPIAccessTokenBindingName, AppStudioE2EApplicationsNamespace)
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
		component, err := framework.HasController.CreateComponentFromStub(compDetected, componentName, AppStudioE2EApplicationsNamespace, SPIAccessTokenSecretName, applicationName)
		Expect(err).NotTo(HaveOccurred())
		Expect(component.Name).To(Equal(componentName))
	})
})

// createAndInjectTokenToSPI creates the specified SPIAccessTokenBinding resource and injects the specified token into it.
func createAndInjectTokenToSPI(framework *framework.Framework, token, repoURL, spiAccessTokenBindingName, namespace, secretName string) {
	// Get the token for the current openshift user
	tokenBytes, err := exec.Command("oc", "whoami", "--show-token").Output()
	Expect(err).NotTo(HaveOccurred())
	bearerToken := strings.TrimSuffix(string(tokenBytes), "\n")

	// Create the SPI Access Token Binding resource and upload the token for the private repository
	_, err = framework.SPIController.CreateSPIAccessTokenBinding(spiAccessTokenBindingName, namespace, PrivateQuarkusDevfileSource, secretName)
	Expect(err).NotTo(HaveOccurred())

	// Wait for the resource to be in the "AwaitingTokenData" phase
	var spiAccessTokenBinding *v1beta1.SPIAccessTokenBinding
	Eventually(func() bool {
		// application info should be stored even after deleting the application in application variable
		spiAccessTokenBinding, err = framework.SPIController.GetSPIAccessTokenBinding(spiAccessTokenBindingName, namespace)
		if err != nil {
			return false
		}
		return spiAccessTokenBinding.Status.Phase == v1beta1.SPIAccessTokenBindingPhaseInjected || (spiAccessTokenBinding.Status.Phase == v1beta1.SPIAccessTokenBindingPhaseAwaitingTokenData && spiAccessTokenBinding.Status.OAuthUrl != "")
	}, 1*time.Minute, 100*time.Millisecond).Should(BeTrue(), "SPI controller didn't set SPIAccessTokenBinding to AwaitingTokenData/Injected")

	if spiAccessTokenBinding.Status.Phase == v1beta1.SPIAccessTokenBindingPhaseAwaitingTokenData {
		// If the phase is AwaitingTokenData then manually inject the git token
		// Get the oauth url and linkedAccessTokenName from the spiaccesstokenbinding resource
		oauthURL := spiAccessTokenBinding.Status.OAuthUrl
		parsedOAuthURL, err := url.Parse(oauthURL)
		Expect(err).NotTo(HaveOccurred())
		oauthHost := parsedOAuthURL.Host
		linkedAccessTokenName := spiAccessTokenBinding.Status.LinkedAccessTokenName

		// Before injecting the token, validate that the linkedaccesstoken resource exists, otherwise injecting will return a 404 error code
		Eventually(func() bool {
			// application info should be stored even after deleting the application in application variable
			_, err := framework.SPIController.GetSPIAccessToken(linkedAccessTokenName, namespace)
			return err == nil
		}, 1*time.Minute, 100*time.Millisecond).Should(BeTrue(), "SPI controller didn't create the SPIAccessToken")

		// Now that the spiaccesstokenbinding is in the AwaitingTokenData phase, inject the GitHub token
		var bearer = "Bearer " + string(bearerToken)
		var jsonStr = []byte(`{"access_token":"` + token + `"}`)
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		req, err := http.NewRequest("POST", "https://"+oauthHost+"/token/"+namespace+"/"+linkedAccessTokenName, bytes.NewBuffer(jsonStr))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Add("Authorization", bearer)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).Should(Equal(204))
		defer resp.Body.Close()

		// Check to see if the token was successfully injected
		Eventually(func() bool {
			// application info should be stored even after deleting the application in application variable
			spiAccessTokenBinding, err = framework.SPIController.GetSPIAccessTokenBinding(spiAccessTokenBindingName, namespace)
			return err == nil && spiAccessTokenBinding.Status.Phase == v1beta1.SPIAccessTokenBindingPhaseInjected
		}, 1*time.Minute, 100*time.Millisecond).Should(BeTrue(), "SPI controller didn't set SPIAccessTokenBinding to Injected")
	}

}
