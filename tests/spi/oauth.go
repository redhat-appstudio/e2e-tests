package spi

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/service-provider-integration-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

/*
 * Component: spi
 * Description: SVPI-395 - Github OAuth flow to upload token
 */

var _ = framework.SPISuiteDescribe(Label("spi-suite", "gh-oauth-flow"), func() {

	defer GinkgoRecover()

	var fw *framework.Framework
	var err error
	var namespace string
	var CYPRESS_GH_USER string
	var CYPRESS_GH_PASSWORD string
	var CYPRESS_GH_2FA_CODE string
	var CYPRESS_SPI_LOGIN_URL string
	var CYPRESS_K8S_TOKEN string
	var cypressPodName string = "cypress-script"
	AfterEach(framework.ReportFailure(&fw))

	// TODO: skip until https://issues.redhat.com/browse/KFLUXBUGS-1108 is fixed
	Describe("SVPI-395 - Github OAuth flow to upload token", Pending, Ordered, func() {
		BeforeAll(func() {

			if os.Getenv("CI") != "true" {
				Skip(fmt.Sprintln("test skipped on local execution"))
			}
			// Initialize the tests controllers
			fw, err = framework.NewFramework(utils.GetGeneratedNamespace("spi-demos-oauth"))
			Expect(err).NotTo(HaveOccurred())
			namespace = fw.UserNamespace
			Expect(namespace).NotTo(BeEmpty())

			CYPRESS_GH_USER = utils.GetEnv("CYPRESS_GH_USER", "")
			Expect(CYPRESS_GH_USER).NotTo(BeEmpty(), "Please provide CYPRESS_GH_USER")

			CYPRESS_GH_PASSWORD = utils.GetEnv("CYPRESS_GH_PASSWORD", "")
			Expect(CYPRESS_GH_PASSWORD).NotTo(BeEmpty(), "Please provide CYPRESS_GH_PASSWORD")

			CYPRESS_GH_2FA_CODE = utils.GetEnv("CYPRESS_GH_2FA_CODE", "")
			Expect(CYPRESS_GH_2FA_CODE).NotTo(BeEmpty(), "Please provide CYPRESS_GH_2FA_CODE env")

		})

		// Clean up after running these tests and before the next tests block: can't have multiple AccessTokens in Injected phase
		AfterAll(func() {

			artifactDir := utils.GetEnv("ARTIFACT_DIR", "")
			if artifactDir != "" {
				// collect cypress recording from the pod and save it in the artifacts folder
				err := utils.ExecuteCommandInASpecificDirectory("kubectl", []string{"cp", cypressPodName + ":/cypress-browser-oauth-flow/cypress/videos", artifactDir + "/cypress/spi-oauth/", "-n", namespace}, "")
				if err != nil {
					klog.Infof("cannot save screen recording in the artifacts folder: %s", err)
				}
			}

			if !CurrentSpecReport().Failed() {
				Expect(fw.AsKubeAdmin.SPIController.DeleteAllBindingTokensInASpecificNamespace(namespace)).To(Succeed())
				Expect(fw.AsKubeAdmin.SPIController.DeleteAllAccessTokensInASpecificNamespace(namespace)).To(Succeed())
			}
		})

		var SPITokenBinding *v1beta1.SPIAccessTokenBinding
		var CYPRESS_SPI_OAUTH_URL string
		tokenBindingName := "spi-token-binding-oauth-"
		OAUTH_REDIRECT_PROXY_URL := utils.GetEnv("OAUTH_REDIRECT_PROXY_URL", "")

		if utils.GetEnv("CI", "") == "true" {
			/*
				If we are running this test in CI, we need to handle the dynamic url the cluster is assigned with.
				To do that, we use a redirect proxy that allows us to have a static oauth url in the providers configuration and, at the same time,
				will redirect the callback call to the spi component in our cluster. OAUTH_REDIRECT_PROXY_URL env should contains the url of such proxy.
				If not running in CI, SPI expects that the callback url in the provider configuration is set to the default one: homepage URL + /oauth/callback
			*/
			It("ensure OauthRedirectProxyUrl is set", func() {

				Expect(OAUTH_REDIRECT_PROXY_URL).NotTo(BeEmpty(), "OAUTH_REDIRECT_PROXY_URL env is not set")

				spiNamespace := "spi-system"
				config, err := fw.AsKubeAdmin.CommonController.GetConfigMap("spi-oauth-service-environment-config", spiNamespace)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Data["OAUTH_REDIRECT_PROXY_URL"]).NotTo(BeEmpty())

			})
		}

		It("creates SPITokenBinding", func() {
			SPITokenBinding, err = fw.AsKubeDeveloper.SPIController.CreateSPIAccessTokenBinding(tokenBindingName, namespace, RepoURL, "", "kubernetes.io/basic-auth")
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				SPITokenBinding, err = fw.AsKubeDeveloper.SPIController.GetSPIAccessTokenBinding(SPITokenBinding.Name, namespace)
				Expect(err).NotTo(HaveOccurred())

				return (SPITokenBinding.Status.OAuthUrl != "")
			}, 1*time.Minute, 10*time.Second).Should(BeTrue(), "OAuthUrl should not be empty")

			CYPRESS_SPI_OAUTH_URL = SPITokenBinding.Status.OAuthUrl
			Expect(CYPRESS_SPI_OAUTH_URL).NotTo(BeEmpty())

			k8s_token, err := utils.GetOpenshiftToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(k8s_token).NotTo(BeEmpty())

			// Build the urls to login to spi
			// An HTML form is server by the redirect-proxy to submit the k8s_token via spi POST /login endpoint
			spi_oauth_url, err := url.Parse(CYPRESS_SPI_OAUTH_URL)
			Expect(err).NotTo(HaveOccurred())
			oauth_redirect_proxy_url, err := url.Parse(OAUTH_REDIRECT_PROXY_URL)
			Expect(err).NotTo(HaveOccurred())

			login_redirect_proxy_url := oauth_redirect_proxy_url.Scheme + "://" + oauth_redirect_proxy_url.Host + "/logiiiin"
			spi_login_url := spi_oauth_url.Scheme + "://" + spi_oauth_url.Host + "/logiiiin"

			CYPRESS_SPI_LOGIN_URL = login_redirect_proxy_url + "?url=" + spi_login_url
			CYPRESS_K8S_TOKEN = k8s_token
		})

		It("run browser oauth login flow in cypress pod", func() {

			// Now we create a short-living pod that will use cypress to perform the browser login flow
			cypressPod := &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cypressPodName,
					Namespace: namespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    cypressPodName,
							Image:   "quay.io/redhat-appstudio-qe/cypress/included:latest",
							Command: []string{"/bin/sh", "-c"},
							Args:    []string{"git clone https://github.com/redhat-appstudio-qe/cypress-browser-oauth-flow; cd cypress-browser-oauth-flow; npm install; echo HELLLOOOOOO; cypress run --spec cypress/e2e/spec.cy.js; tail -f /dev/null;"},
							Env: []corev1.EnvVar{
								{
									Name:  "CYPRESS_GH_USER",
									Value: CYPRESS_GH_USER,
								},
								{
									Name:  "CYPRESS_GH_PASSWORD",
									Value: CYPRESS_GH_PASSWORD,
								},
								{
									Name:  "CYPRESS_GH_2FA_CODE",
									Value: CYPRESS_GH_2FA_CODE,
								},
								{
									Name:  "CYPRESS_SPI_OAUTH_URL",
									Value: CYPRESS_SPI_OAUTH_URL,
								},
								{
									Name:  "CYPRESS_SPI_LOGIN_URL",
									Value: CYPRESS_SPI_LOGIN_URL,
								},
								{
									Name:  "CYPRESS_K8S_TOKEN",
									Value: CYPRESS_K8S_TOKEN,
								},
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}

			_, err := fw.AsKubeAdmin.CommonController.KubeInterface().CoreV1().Pods(namespace).Create(context.Background(), cypressPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// check pod is running
			// if spi oauth flow is completed, the SPITokenBinding will be injected
			// keeping the pod running and only checking the SPITokenBinding (instead of the pod status itself) allows us
			// to get the logs and browser session recording from the cypress pod.
			Eventually(func() bool {
				pod, err := fw.AsKubeAdmin.CommonController.GetPod(namespace, cypressPod.Name)
				Expect(err).NotTo(HaveOccurred())

				return (pod.Status.Phase == corev1.PodRunning)
			}, 15*time.Minute, 5*time.Second).Should(BeTrue(), "Cypress pod did not start")

		})

		It("SPITokenBinding should be in Injected phase", func() {
			Eventually(func() bool {
				SPITokenBinding, err = fw.AsKubeDeveloper.SPIController.GetSPIAccessTokenBinding(SPITokenBinding.Name, namespace)
				Expect(err).NotTo(HaveOccurred())

				return SPITokenBinding.Status.Phase == v1beta1.SPIAccessTokenBindingPhaseInjected
			}, 15*time.Minute, 10*time.Second).Should(BeTrue(), "SPIAccessTokenBinding is not in Injected phase after Oauth flow")
		})

	})
})
