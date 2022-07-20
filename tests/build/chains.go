package build

import (
	"fmt"
	"time"

	"github.com/devfile/library/pkg/util"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/tekton"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
)

var _ = framework.ChainsSuiteDescribe("Tekton Chains E2E tests", func() {
	defer g.GinkgoRecover()

	// Set this to true to skip contract tests
	var skipContract bool = false
	var skipContractMsg string = "Temporarily disabling until the EC task definition is updated"

	// Initialize the tests controllers
	framework, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	g.Context("infrastructure is running", func() {
		g.It("verify the chains controller is running", func() {
			err := framework.CommonController.WaitForPodSelector(framework.CommonController.IsPodRunning, constants.TEKTON_CHAINS_NS, "app", "tekton-chains-controller", 60, 100)
			Expect(err).NotTo(HaveOccurred())
		})
		g.It("verify the correct secrets have been created", func() {
			_, err := framework.CommonController.GetSecret(constants.TEKTON_CHAINS_NS, "chains-ca-cert")
			Expect(err).NotTo(HaveOccurred())
		})
		g.It("verify the correct roles are created", func() {
			_, csaErr := framework.CommonController.GetRole("chains-secret-admin", constants.TEKTON_CHAINS_NS)
			Expect(csaErr).NotTo(HaveOccurred())
			_, srErr := framework.CommonController.GetRole("secret-reader", "openshift-ingress-operator")
			Expect(srErr).NotTo(HaveOccurred())
		})
		g.It("verify the correct rolebindings are created", func() {
			_, csaErr := framework.CommonController.GetRoleBinding("chains-secret-admin", constants.TEKTON_CHAINS_NS)
			Expect(csaErr).NotTo(HaveOccurred())
			_, csrErr := framework.CommonController.GetRoleBinding("chains-secret-reader", "openshift-ingress-operator")
			Expect(csrErr).NotTo(HaveOccurred())
		})
		g.It("verify the correct service account is created", func() {
			_, err := framework.CommonController.GetServiceAccount("chains-secrets-admin", constants.TEKTON_CHAINS_NS)
			Expect(err).NotTo(HaveOccurred())
		})
	})
	g.Context("test creating and signing an image and task", func() {
		// Make the TaskRun name and namespace predictable. For convenience, the name of the
		// TaskRun that builds an image, is the same as the repository where the image is
		// pushed to.
		namespace := "tekton-chains"
		buildPipelineRunName := fmt.Sprintf("buildah-demo-%s", util.GenerateRandomString(10))
		image := fmt.Sprintf("image-registry.openshift-image-registry.svc:5000/%s/%s", namespace, buildPipelineRunName)

		pipelineRunTimeout := 360
		attestationTimeout := time.Duration(60) * time.Second
		kubeController := tekton.KubeController{
			Commonctrl: *framework.CommonController,
			Tektonctrl: *framework.TektonController,
			Namespace:  constants.TEKTON_CHAINS_NS,
		}

		var imageWithDigest string

		g.BeforeAll(func() {
			// At a bare minimum, each spec within this context relies on the existence of
			// an image that has been signed by Tekton Chains. Trigger a demo task to fulfill
			// this purpose.
			pr, err := kubeController.RunPipeline(tekton.BuildahDemo{Image: image, Bundle: framework.TektonController.Bundles.BuildTemplatesBundle}, pipelineRunTimeout)
			Expect(err).NotTo(HaveOccurred())
			// Verify that the build task was created as expected.
			Expect(buildPipelineRunName).To(Equal(pr.ObjectMeta.Name))
			Expect(namespace).To(Equal(pr.ObjectMeta.Namespace))
			Expect(kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)).To(Succeed())
			g.GinkgoWriter.Printf("The pipeline run is %q under namespace %q\n", pr.ObjectMeta.Name, pr.ObjectMeta.Namespace)

			// The TaskRun resource has been updated, refresh our reference.
			pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.ObjectMeta.Name, pr.ObjectMeta.Namespace)
			Expect(err).NotTo(HaveOccurred())

			// Verify TaskRun has the type hinting required by Tekton Chains
			digest, err := kubeController.GetTaskRunResult(pr, "build-container", "IMAGE_DIGEST")
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("%s", err))
			Expect(kubeController.GetTaskRunResult(pr, "build-container", "IMAGE_URL")).To(Equal(image))
			g.GinkgoWriter.Printf("The image signed by Tekton Chains is %s@%s\n", image, digest)

			// Specs now have a deterministic image reference for validation \o/
			imageWithDigest = fmt.Sprintf("%s@%s", image, digest)
		})

		g.It("creates signature and attestation", func() {
			err = kubeController.AwaitAttestationAndSignature(imageWithDigest, attestationTimeout)
			Expect(err).NotTo(
				HaveOccurred(),
				"Could not find .att or .sig ImageStreamTags within the %s timeout. "+
					"Most likely the chains-controller did not create those in time. "+
					"Look at the chains-controller logs.",
				attestationTimeout.String(),
			)
			g.GinkgoWriter.Println("Cosign verify pass with .att and .sig ImageStreamTags found")
		})

		g.Context("verify-enterprise-contract task", func() {
			var generator tekton.VerifyEnterpriseContract
			var rekorHost string
			publicSecretName := "cosign-public-key"

			g.BeforeAll(func() {
				// Copy the public key from tekton-chains/signing-secrets to a new
				// secret that contains just the public key to ensure that access
				// to password and private key are not needed.
				publicKey, err := kubeController.GetPublicKey("signing-secrets", "tekton-chains")
				Expect(err).ToNot(HaveOccurred())
				g.GinkgoWriter.Println("Copy public key from tekton-chains/signing-secrets to a new secret")
				Expect(kubeController.CreateOrUpdateSigningSecret(
					publicKey, publicSecretName, namespace)).To(Succeed())

				rekorHost, err = kubeController.GetRekorHost()
				Expect(err).ToNot(HaveOccurred())
			})

			g.BeforeEach(func() {
				generator = tekton.VerifyEnterpriseContract{
					PipelineRunName: "verify-enterprise-contract",
					ImageRef:        imageWithDigest,
					PublicSecret:    fmt.Sprintf("k8s://%s/%s", namespace, publicSecretName),
					PipelineName:    "pipeline-run-that-does-not-exist",
					RekorHost:       rekorHost,
					SslCertDir:      "/var/run/secrets/kubernetes.io/serviceaccount",
					StrictPolicy:    "1",
					Bundle:          framework.TektonController.Bundles.HACBSTemplatesBundle,
				}

				// Since specs could update the config policy, make sure it has a consistent
				// baseline at the start of each spec.
				baselinePolicies := `{"non_blocking_checks":["not_useful"]}`
				g.GinkgoWriter.Printf("Set the non-blocking checks to baseline policies: %s\n", baselinePolicies)
				Expect(kubeController.CreateOrUpdateConfigPolicy(
					namespace, baselinePolicies)).To(Succeed())
			})

			g.It("succeeds when policy is met", func() {
				if skipContract {
					g.Skip(skipContractMsg)
				}
				// Setup a policy config to ignore the policy check for tests
				policies := `{"non_blocking_checks":["not_useful", "test"]}`
				g.GinkgoWriter.Printf("Set the non-blocking checks to policies: %s\n", policies)
				Expect(kubeController.CreateOrUpdateConfigPolicy(
					namespace, policies)).To(Succeed())
				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				Expect(kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)).To(Succeed())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				g.GinkgoWriter.Printf("Make sure task %q has passed\n", pr.Name)
				Expect(tr.Status.TaskRunResults).Should(ContainElements(
					tekton.MatchTaskRunResultWithJSONValue("OUTPUT", `[
						{
							"filename": "/shared/ec-work-dir/input/input.json",
							"namespace": "release.main",
							"successes": 2
						}
					]`),
					tekton.MatchTaskRunResult("PASSED", "true"),
				))
			})

			g.It("does not pass when tests are not satisfied on non-strict mode", func() {
				if skipContract {
					g.Skip(skipContractMsg)
				}
				generator.StrictPolicy = "0"
				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				Expect(kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)).To(Succeed())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				g.GinkgoWriter.Printf("Make sure pipeline %q has failed\n", pr.Name)
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				Expect(tr.Status.TaskRunResults).Should(ContainElements(
					tekton.MatchTaskRunResultWithJSONValue("OUTPUT", `[
						{
							"filename": "/shared/ec-work-dir/input/input.json",
							"namespace": "release.main",
							"successes": 1,
							"failures": [
								{
									"msg": "No test data found",
									"metadata": {
										"code": "test_data_missing",
										"effective_on": "2022-01-01T00:00:00Z"
									}
								}
							]
						}
					]`),
					tekton.MatchTaskRunResult("PASSED", "false"),
				))
			})

			g.It("fails when tests are not satisfied on strict mode", func() {
				if skipContract {
					g.Skip(skipContractMsg)
				}
				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				err = kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				g.GinkgoWriter.Printf("Make sure pipeline %q has failed\n", pr.Name)
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				Expect(tr.Status.GetCondition("Succeeded").IsTrue()).To(BeFalse())
				// Because the task fails, no results are created
			})

			g.It("fails when unexpected signature is used", func() {
				if skipContract {
					g.Skip(skipContractMsg)
				}
				secretName := fmt.Sprintf("dummy-public-key-%s", util.GenerateRandomString(10))
				publicKey := []byte("-----BEGIN PUBLIC KEY-----\n" +
					"MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAENZxkE/d0fKvJ51dXHQmxXaRMTtVz\n" +
					"BQWcmJD/7pcMDEmBcmk8O1yUPIiFj5TMZqabjS9CQQN+jKHG+Bfi0BYlHg==\n" +
					"-----END PUBLIC KEY-----")
				g.GinkgoWriter.Println("Create an unexpected public signing key")
				Expect(kubeController.CreateOrUpdateSigningSecret(publicKey, secretName, namespace)).To(Succeed())
				generator.PublicSecret = fmt.Sprintf("k8s://%s/%s", namespace, secretName)

				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				err = kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				g.GinkgoWriter.Printf("Make sure pipeline %q has failed\n", pr.Name)
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				Expect(tr.Status.GetCondition("Succeeded").IsTrue()).To(BeFalse())
				// Because the task fails, no results are created
			})
		})

	})
})
