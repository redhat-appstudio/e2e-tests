package build

import (
	"context"
	"fmt"
	"time"

	"github.com/devfile/library/pkg/util"
	ecp "github.com/hacbs-contract/enterprise-contract-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/common"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/tekton"
)

var _ = framework.ChainsSuiteDescribe("Tekton Chains E2E tests", func() {
	defer GinkgoRecover()

	// Initialize the tests controllers
	framework, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	Context("infrastructure is running", func() {
		It("verify the chains controller is running", func() {
			err := framework.CommonController.WaitForPodSelector(framework.CommonController.IsPodRunning, constants.TEKTON_CHAINS_NS, "app", "tekton-chains-controller", 60, 100)
			Expect(err).NotTo(HaveOccurred())
		})
		It("verify the correct secrets have been created", func() {
			_, err := framework.CommonController.GetSecret(constants.TEKTON_CHAINS_NS, "chains-ca-cert")
			Expect(err).NotTo(HaveOccurred())
		})
		It("verify the correct roles are created", func() {
			_, csaErr := framework.CommonController.GetRole("chains-secret-admin", constants.TEKTON_CHAINS_NS)
			Expect(csaErr).NotTo(HaveOccurred())
			_, srErr := framework.CommonController.GetRole("secret-reader", "openshift-ingress-operator")
			Expect(srErr).NotTo(HaveOccurred())
		})
		It("verify the correct rolebindings are created", func() {
			_, csaErr := framework.CommonController.GetRoleBinding("chains-secret-admin", constants.TEKTON_CHAINS_NS)
			Expect(csaErr).NotTo(HaveOccurred())
			_, csrErr := framework.CommonController.GetRoleBinding("chains-secret-reader", "openshift-ingress-operator")
			Expect(csrErr).NotTo(HaveOccurred())
		})
		It("verify the correct service account is created", func() {
			_, err := framework.CommonController.GetServiceAccount("chains-secrets-admin", constants.TEKTON_CHAINS_NS)
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("test creating and signing an image and task", func() {
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

		// the default policy source
		rev := "main"
		policySource := ecp.GitPolicySource{
			Repository: "https://github.com/hacbs-contract/ec-policies.git",
			Revision:   &rev,
		}

		// if there is a ConfigMap e2e-tests/ec-config with keys `revision` and
		// `repository` values from those will replace the default policy source
		// this gives us a way to set the tests to use a different policy if we
		// break the tests in the default policy source
		if config, err := framework.CommonController.K8sClient.KubeInterface().CoreV1().ConfigMaps("e2e-tests").Get(context.TODO(), "ec-config", v1.GetOptions{}); err != nil {
			if v, ok := config.Data["revision"]; ok {
				policySource.Revision = &v
			}
			if v, ok := config.Data["repository"]; ok {
				policySource.Repository = v
			}
		}

		var imageWithDigest string

		BeforeAll(func() {
			// At a bare minimum, each spec within this context relies on the existence of
			// an image that has been signed by Tekton Chains. Trigger a demo task to fulfill
			// this purpose.
			pr, err := kubeController.RunPipeline(tekton.BuildahDemo{Image: image, Bundle: framework.TektonController.Bundles.BuildTemplatesBundle}, pipelineRunTimeout)
			Expect(err).NotTo(HaveOccurred())
			// Verify that the build task was created as expected.
			Expect(pr.ObjectMeta.Name).To(Equal(buildPipelineRunName))
			Expect(pr.ObjectMeta.Namespace).To(Equal(namespace))
			Expect(kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)).To(Succeed())
			GinkgoWriter.Printf("The pipeline named %q in namespace %q suceeded\n", pr.ObjectMeta.Name, pr.ObjectMeta.Namespace)

			// The TaskRun resource has been updated, refresh our reference.
			pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.ObjectMeta.Name, pr.ObjectMeta.Namespace)
			Expect(err).NotTo(HaveOccurred())

			// Verify TaskRun has the type hinting required by Tekton Chains
			digest, err := kubeController.GetTaskRunResult(pr, "build-container", "IMAGE_DIGEST")
			Expect(err).NotTo(HaveOccurred())
			i, err := kubeController.GetTaskRunResult(pr, "build-container", "IMAGE_URL")
			Expect(err).NotTo(HaveOccurred())
			Expect(i).To(Equal(image))

			// Specs now have a deterministic image reference for validation \o/
			imageWithDigest = fmt.Sprintf("%s@%s", image, digest)

			GinkgoWriter.Printf("The image signed by Tekton Chains is %s\n", imageWithDigest)
		})

		It("creates signature and attestation", func() {
			err = kubeController.AwaitAttestationAndSignature(imageWithDigest, attestationTimeout)
			Expect(err).NotTo(
				HaveOccurred(),
				"Could not find .att or .sig ImageStreamTags within the %s timeout. "+
					"Most likely the chains-controller did not create those in time. "+
					"Look at the chains-controller logs.",
				attestationTimeout.String(),
			)
			GinkgoWriter.Printf("Cosign verify pass with .att and .sig ImageStreamTags found for %s\n", imageWithDigest)
		})

		Context("verify-enterprise-contract task", func() {
			var generator tekton.VerifyEnterpriseContract
			var rekorHost string
			publicSecretName := "cosign-public-key"

			BeforeAll(func() {
				// Copy the public key from tekton-chains/signing-secrets to a new
				// secret that contains just the public key to ensure that access
				// to password and private key are not needed.
				publicKey, err := kubeController.GetPublicKey("signing-secrets", "tekton-chains")
				Expect(err).ToNot(HaveOccurred())
				GinkgoWriter.Println("Copy public key from tekton-chains/signing-secrets to a new secret")
				Expect(kubeController.CreateOrUpdateSigningSecret(
					publicKey, publicSecretName, namespace)).To(Succeed())

				rekorHost, err = kubeController.GetRekorHost()
				Expect(err).ToNot(HaveOccurred())
				GinkgoWriter.Printf("Configured Rekor host: %s\n", rekorHost)
			})

			BeforeEach(func() {
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
				baselinePolicies := ecp.EnterpriseContractPolicySpec{
					Sources: []ecp.PolicySource{
						{
							GitRepository: &policySource,
						},
					},
					Exceptions: &ecp.EnterpriseContractPolicyExceptions{
						NonBlocking: []string{}, // verify-enterprise-contract-v1 will fail unless we have something here
					},
				}
				Expect(kubeController.CreateOrUpdatePolicyConfiguration(namespace, baselinePolicies)).To(Succeed())
				printPolicyConfiguration(baselinePolicies)
			})

			It("succeeds when policy is met", func() {
				// Setup a policy config to ignore the policy check for tests
				policy := ecp.EnterpriseContractPolicySpec{
					Sources: []ecp.PolicySource{
						{
							GitRepository: &policySource,
						},
					},
					Exceptions: &ecp.EnterpriseContractPolicyExceptions{
						NonBlocking: []string{"not_useful", "test", "tasks"},
					},
				}
				Expect(kubeController.CreateOrUpdatePolicyConfiguration(namespace, policy)).To(Succeed())
				printPolicyConfiguration(policy)
				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				Expect(kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)).To(Succeed())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				printTaskRunStatus(tr, namespace, *framework.CommonController)
				GinkgoWriter.Printf("Make sure TaskRun of PipelineRun %s suceeded\n", pr.Name)
				Expect(tekton.DidTaskSucceed(tr)).To(BeTrue())
				GinkgoWriter.Printf("Make sure EC results for PipelineRun %s are passing\n", pr.Name)
				Expect(tr.Status.TaskRunResults).Should(ContainElements(
					tekton.MatchTaskRunResult("PASSED", "true"),
				))
			})

			It("does not pass when tests are not satisfied on non-strict mode", func() {
				generator.StrictPolicy = "0"
				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				Expect(kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)).To(Succeed())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				printTaskRunStatus(tr, namespace, *framework.CommonController)
				GinkgoWriter.Printf("Make sure TaskRun %q has not suceeded\n", pr.Name)
				Expect(tekton.DidTaskSucceed(tr)).To(BeFalse())
			})

			It("fails when tests are not satisfied on strict mode", func() {
				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				err = kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("Task run status %#v\n", tr.Status)
				GinkgoWriter.Printf("Make sure pipeline %q has not suceeded\n", pr.Name)
				Expect(tekton.DidTaskSucceed(tr)).To(BeFalse())
				// Because the task fails, no results are created
			})

			It("fails when unexpected signature is used", func() {
				secretName := fmt.Sprintf("dummy-public-key-%s", util.GenerateRandomString(10))
				publicKey := []byte("-----BEGIN PUBLIC KEY-----\n" +
					"MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAENZxkE/d0fKvJ51dXHQmxXaRMTtVz\n" +
					"BQWcmJD/7pcMDEmBcmk8O1yUPIiFj5TMZqabjS9CQQN+jKHG+Bfi0BYlHg==\n" +
					"-----END PUBLIC KEY-----")
				GinkgoWriter.Println("Create an unexpected public signing key")
				Expect(kubeController.CreateOrUpdateSigningSecret(publicKey, secretName, namespace)).To(Succeed())
				generator.PublicSecret = fmt.Sprintf("k8s://%s/%s", namespace, secretName)

				pr, err := kubeController.RunPipeline(generator, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())
				err = kubeController.WatchPipelineRun(pr.Name, pipelineRunTimeout)
				Expect(err).NotTo(HaveOccurred())

				// Refresh our copy of the PipelineRun for latest results
				pr, err = kubeController.Tektonctrl.GetPipelineRun(pr.Name, pr.Namespace)
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("Make sure pipeline %q has failed\n", pr.Name)
				tr, err := kubeController.GetTaskRunStatus(pr, "verify-enterprise-contract")
				Expect(err).NotTo(HaveOccurred())
				Expect(tr.Status.GetCondition("Succeeded").IsTrue()).To(BeFalse())
				// Because the task fails, no results are created
			})
		})

	})
})

func printPolicyConfiguration(policy ecp.EnterpriseContractPolicySpec) {
	sources := ""
	for i, s := range policy.Sources {
		if i != 0 {
			sources += "\n"
		}
		if s.GitRepository != nil {
			if s.GitRepository.Revision != nil {
				sources += fmt.Sprintf("[%d] repository: '%s', revision: '%s'", i, s.GitRepository.Repository, *s.GitRepository.Revision)
			} else {
				sources += fmt.Sprintf("[%d] repository: '%s'", i, s.GitRepository.Repository)
			}
		}
	}
	exceptions := "[]"
	if policy.Exceptions != nil {
		exceptions = fmt.Sprintf("%v", policy.Exceptions.NonBlocking)
	}
	GinkgoWriter.Printf("Configured sources: %s\nand non-blocking policies: %v\n", sources, exceptions)
}

func printTaskRunStatus(tr *v1beta1.PipelineRunTaskRunStatus, namespace string, sc common.SuiteController) {
	if tr.Status == nil {
		GinkgoWriter.Printf("*** TaskRun status: nil")
		return
	}

	if y, err := yaml.Marshal(tr.Status); err == nil {
		GinkgoWriter.Printf("*** TaskRun status:\n%s\n", string(y))
	} else {
		GinkgoWriter.Printf("*** Unable to serialize TaskRunStatus to YAML: %#v; error: %s", tr.Status, err)
	}

	for _, s := range tr.Status.TaskRunStatusFields.Steps {
		if logs, err := sc.GetContainerLogs(tr.Status.PodName, s.ContainerName, namespace); err == nil {
			GinkgoWriter.Printf("*** Logs from pod '%s', container '%s':\n----- START -----%s----- END -----\n", tr.Status.PodName, s.ContainerName, logs)
		} else {
			GinkgoWriter.Printf("*** Can't fetch logs from pod '%s', container '%s': %s", tr.Status.PodName, s.ContainerName, err)
		}
	}
}
