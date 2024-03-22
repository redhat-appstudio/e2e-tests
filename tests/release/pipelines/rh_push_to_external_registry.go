package pipelines

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/has"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/release"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/contract"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/tekton"
	releasecommon "github.com/redhat-appstudio/e2e-tests/tests/release"
	releaseApi "github.com/redhat-appstudio/release-service/api/v1alpha1"
	tektonutils "github.com/redhat-appstudio/release-service/tekton/utils"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = framework.ReleasePipelinesSuiteDescribe("[HACBS-1571]test-release-e2e-push-image-to-pyxis", Label("release-pipelines", "pushPyxis", "HACBS"), func() {
	defer GinkgoRecover()
	// Initialize the tests controllers
	var fw *framework.Framework
	AfterEach(framework.ReportFailure(&fw))
	var err error
	var devNamespace, managedNamespace, compName, additionalCompName string
	var avgControllerQueryTimeout = 5 * time.Minute

	var imageIDs []string
	var pyxisKeyDecoded, pyxisCertDecoded []byte
	var releasePR1, releasePR2 *pipeline.PipelineRun
	scGitRevision := fmt.Sprintf("test-pyxis-%s", util.GenerateRandomString(4))

	var component1, component2 *appservice.Component
	var snapshot1, snapshot2 *appservice.Snapshot
	var releaseCR1, releaseCR2 *releaseApi.Release

	var componentDetected, additionalComponentDetected appservice.ComponentDetectionDescription

	BeforeAll(func() {
		fw, err = framework.NewFramework(utils.GetGeneratedNamespace("push-pyxis"))
		Expect(err).NotTo(HaveOccurred())
		devNamespace = fw.UserNamespace
		managedNamespace = utils.GetGeneratedNamespace("push-pyxis-managed")

		_, err = fw.AsKubeAdmin.CommonController.CreateTestNamespace(managedNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating managedNamespace")

		sourceAuthJson := utils.GetEnv("QUAY_TOKEN", "")
		Expect(sourceAuthJson).ToNot(BeEmpty())

		keyPyxisStage := os.Getenv(constants.PYXIS_STAGE_KEY_ENV)
		Expect(keyPyxisStage).ToNot(BeEmpty())

		certPyxisStage := os.Getenv(constants.PYXIS_STAGE_CERT_ENV)
		Expect(certPyxisStage).ToNot(BeEmpty())

		// Create secret for the release registry repo "hacbs-release-tests".
		_, err = fw.AsKubeAdmin.CommonController.CreateRegistryAuthSecret(releasecommon.RedhatAppstudioUserSecret, managedNamespace, sourceAuthJson)
		Expect(err).ToNot(HaveOccurred())

		// Linking the build secret to the pipeline service account in dev namespace.
		err = fw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(devNamespace, releasecommon.HacbsReleaseTestsTokenSecret, constants.DefaultPipelineServiceAccount, true)
		Expect(err).ToNot(HaveOccurred())

		publicKey, err := fw.AsKubeAdmin.TektonController.GetTektonChainsPublicKey()
		Expect(err).ToNot(HaveOccurred())

		// Creating k8s secret to access Pyxis stage based on base64 decoded of key and cert
		pyxisKeyDecoded, err = base64.StdEncoding.DecodeString(string(keyPyxisStage))
		Expect(err).ToNot(HaveOccurred())

		pyxisCertDecoded, err = base64.StdEncoding.DecodeString(string(certPyxisStage))
		Expect(err).ToNot(HaveOccurred())

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pyxis",
				Namespace: managedNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"cert": pyxisCertDecoded,
				"key":  pyxisKeyDecoded,
			},
		}

		_, err = fw.AsKubeAdmin.CommonController.CreateSecret(managedNamespace, secret)
		Expect(err).ToNot(HaveOccurred())

		Expect(fw.AsKubeAdmin.TektonController.CreateOrUpdateSigningSecret(
			publicKey, releasecommon.PublicSecretNameAuth, managedNamespace)).To(Succeed())

		defaultECP, err := fw.AsKubeAdmin.TektonController.GetEnterpriseContractPolicy("default", "enterprise-contract-service")
		Expect(err).NotTo(HaveOccurred())
		policy := contract.PolicySpecWithSourceConfig(defaultECP.Spec, ecp.SourceConfig{Include: []string{"@minimal"}, Exclude: []string{"cve"}})

		managedServiceAccount, err := fw.AsKubeAdmin.CommonController.CreateServiceAccount(releasecommon.ReleasePipelineServiceAccountDefault, managedNamespace, releasecommon.ManagednamespaceSecret, nil)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePipelineRoleBindingForServiceAccount(managedNamespace, managedServiceAccount)
		Expect(err).NotTo(HaveOccurred())

		err = fw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.RedhatAppstudioUserSecret, releasecommon.ReleasePipelineServiceAccountDefault, true)
		Expect(err).ToNot(HaveOccurred())

		// using cdq since git ref is not known
		cdq, err := fw.AsKubeAdmin.HasController.CreateComponentDetectionQuery(releasecommon.ComponentName, devNamespace, releasecommon.GitSourceComponentUrl, "", "", "", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

		for _, compDetected := range cdq.Status.ComponentDetected {
			compName = compDetected.ComponentStub.ComponentName
			componentDetected = compDetected
		}

		// using cdq since git ref is not known
		additionalCompName = releasecommon.AdditionalComponentName
		cdq, err = fw.AsKubeAdmin.HasController.CreateComponentDetectionQuery(additionalCompName, devNamespace, releasecommon.AdditionalGitSourceComponentUrl, "", "", "", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

		for _, compDetected := range cdq.Status.ComponentDetected {
			additionalCompName = compDetected.ComponentStub.ComponentName
			additionalComponentDetected = compDetected
		}

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(releasecommon.SourceReleasePlanName, devNamespace, releasecommon.ApplicationNameDefault, managedNamespace, "true", nil)
		Expect(err).NotTo(HaveOccurred())

		data, err := json.Marshal(map[string]interface{}{
			"mapping": map[string]interface{}{
				"components": []map[string]interface{}{
					{
						"name":       compName,
						"repository": "quay.io/" + utils.GetQuayIOOrganization() + "/dcmetromap",
					},
					{
						"name":       additionalCompName,
						"repository": "quay.io/" + utils.GetQuayIOOrganization() + "/simplepython",
					},
				},
			},
			"pyxis": map[string]interface{}{
				"server": "stage",
				"secret": "pyxis",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace, "", devNamespace, releasecommon.ReleaseStrategyPolicyDefault, releasecommon.ReleasePipelineServiceAccountDefault, []string{releasecommon.ApplicationNameDefault}, true, &tektonutils.PipelineRef{
			Resolver: "git",
			Params: []tektonutils.Param{
				{Name: "url", Value: releasecommon.RelSvcCatalogURL},
				{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
				{Name: "pathInRepo", Value: "pipelines/rh-push-to-external-registry/rh-push-to-external-registry.yaml"},
			},
		}, &runtime.RawExtension{
			Raw: data,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.TektonController.CreateEnterpriseContractPolicy(releasecommon.ReleaseStrategyPolicyDefault, managedNamespace, policy)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.TektonController.CreatePVCInAccessMode(releasecommon.ReleasePvcName, managedNamespace, corev1.ReadWriteOnce)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.CommonController.CreateRole("role-release-service-account", managedNamespace, map[string][]string{
			"apiGroupsList": {""},
			"roleResources": {"secrets"},
			"roleVerbs":     {"get", "list", "watch"},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.CommonController.CreateRoleBinding("role-release-service-account-binding", managedNamespace, "ServiceAccount", releasecommon.ReleasePipelineServiceAccountDefault, managedNamespace, "Role", "role-release-service-account", "rbac.authorization.k8s.io")
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.HasController.CreateApplication(releasecommon.ApplicationNameDefault, devNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err = fw.AsKubeAdmin.CommonController.Github.DeleteRef(constants.StrategyConfigsRepo, scGitRevision)
		if err != nil {
			Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
		}
		if !CurrentSpecReport().Failed() {
			Expect(fw.AsKubeAdmin.CommonController.DeleteNamespace(managedNamespace)).NotTo(HaveOccurred())
			Expect(fw.SandboxController.DeleteUserSignup(fw.UserName)).To(BeTrue())
		}
	})

	var _ = Describe("Post-release verification", func() {

		It("verifies that Component 1 can be created and build PipelineRun is created for it in dev namespace and succeeds", func() {
			component1, err = fw.AsKubeAdmin.HasController.CreateComponent(componentDetected.ComponentStub, devNamespace, "", "", releasecommon.ApplicationNameDefault, true, map[string]string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component1, "",
				fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
		})

		It("verifies that Component 2 can be created and build PipelineRun is created for it in dev namespace and succeeds", func() {
			component2, err = fw.AsKubeAdmin.HasController.CreateComponent(additionalComponentDetected.ComponentStub, devNamespace, "", "", releasecommon.ApplicationNameDefault, true, map[string]string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component2, "",
				fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
		})

		It("tests that Snapshot is created for each Component", func() {
			Eventually(func() error {
				snapshot1, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", "", component1.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Snapshot for component %s/%s: %v\n", component1.GetNamespace(), component1.GetName(), err)
					return err
				}
				snapshot2, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", "", component2.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Snapshot for component %s/%s: %v\n", component2.GetNamespace(), component2.GetName(), err)
					return err
				}
				return nil
			}, 5*time.Minute, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Snapshots to be created in %s namespace", devNamespace)
		})

		It("tests that associated Release CR is created for each Component's Snapshot", func() {
			Eventually(func() error {
				releaseCR1, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", snapshot1.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Release CR for snapshot %s/%s: %v\n", snapshot1.GetNamespace(), component1.GetName(), err)
					return err
				}
				releaseCR2, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", snapshot2.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Release CR for snapshot %s/%s: %v\n", snapshot2.GetNamespace(), component1.GetName(), err)
					return err
				}
				return nil
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Release CRs to be created in %s namespace", devNamespace)
		})

		It("verifies that Release PipelineRun is triggered for each Release CR", func() {
			Eventually(func() error {
				releasePR1, err = fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseCR1.GetName(), releaseCR1.GetNamespace())
				if err != nil {
					GinkgoWriter.Printf("release pipelineRun for Release %s/%s not created yet: %+v\n", releaseCR1.GetNamespace(), releaseCR1.GetName(), err)
					return err
				}
				releasePR2, err = fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseCR2.GetName(), releaseCR2.GetNamespace())
				if err != nil {
					GinkgoWriter.Printf("release pipelineRun for Release %s/%s not created yet: %+v\n", releaseCR2.GetNamespace(), releaseCR2.GetName(), err)
					return err
				}
				var errMsg string
				for _, pr := range []*pipeline.PipelineRun{releasePR1, releasePR2} {
					Expect(tekton.HasPipelineRunFailed(pr)).ToNot(BeTrue(), fmt.Sprintf("Release PipelineRun %s/%s failed", pr.GetNamespace(), pr.GetName()))
					if !pr.HasStarted() {
						errMsg += fmt.Sprintf("Release PipelineRun %s/%s did not started yet\n", pr.GetNamespace(), pr.GetName())
					}
				}
				if len(errMsg) > 1 {
					return fmt.Errorf(errMsg)
				}
				return nil
			}, releasecommon.ReleasePipelineRunCreationTimeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out waiting for a PipelineRun to start for each Release CR in %s namespace", managedNamespace))
		})

		It("verifies a release PipelineRun for each component succeeded in managed namespace", func() {
			Eventually(func() error {
				var errMsg string
				for _, pr := range []*pipeline.PipelineRun{releasePR1, releasePR2} {
					pr, err = fw.AsKubeAdmin.TektonController.GetPipelineRun(pr.GetName(), pr.GetNamespace())
					if err != nil {
						return err
					}
					Expect(tekton.HasPipelineRunFailed(pr)).ToNot(BeTrue(), fmt.Sprintf("Release PipelineRun %s/%s failed", pr.GetNamespace(), pr.GetName()))
					if pr.IsDone() {
						Expect(tekton.HasPipelineRunSucceeded(pr)).To(BeTrue(), fmt.Sprintf("Release PipelineRun %s/%s did not succceed", pr.GetNamespace(), pr.GetName()))
					} else {
						errMsg += fmt.Sprintf("Release PipelineRun %s/%s did not finish yet\n", pr.GetNamespace(), pr.GetName())
					}
				}
				if len(errMsg) > 1 {
					return fmt.Errorf(errMsg)
				}
				return nil
			}, releasecommon.ReleasePipelineRunCompletionTimeout, constants.PipelineRunPollingInterval).Should(Succeed())
		})

		It("validate the result of task create-pyxis-image contains image ids", func() {
			Eventually(func() []string {
				re := regexp.MustCompile("[a-fA-F0-9]{24}")

				releasePR1, err = fw.AsKubeAdmin.TektonController.GetPipelineRun(releasePR1.GetName(), releasePR1.GetNamespace())
				Expect(err).NotTo(HaveOccurred())
				releasePR2, err = fw.AsKubeAdmin.TektonController.GetPipelineRun(releasePR2.GetName(), releasePR2.GetNamespace())
				Expect(err).NotTo(HaveOccurred())

				trReleasePr, err := fw.AsKubeAdmin.TektonController.GetTaskRunStatus(fw.AsKubeAdmin.CommonController.KubeRest(), releasePR1, "create-pyxis-image")
				Expect(err).NotTo(HaveOccurred())

				trAdditionalReleasePr, err := fw.AsKubeAdmin.TektonController.GetTaskRunStatus(fw.AsKubeAdmin.CommonController.KubeRest(), releasePR2, "create-pyxis-image")
				Expect(err).NotTo(HaveOccurred())

				trReleaseImageIDs := re.FindAllString(trReleasePr.Status.TaskRunStatusFields.Results[0].Value.StringVal, -1)
				trAdditionalReleaseIDs := re.FindAllString(trAdditionalReleasePr.Status.TaskRunStatusFields.Results[0].Value.StringVal, -1)

				Expect(trReleaseImageIDs).ToNot(BeEmpty(), fmt.Sprintf("Invalid ImageID in results of task create-pyxis-image. taskrun results: %+v", trReleasePr.Status.TaskRunStatusFields.Results[0]))
				Expect(trAdditionalReleaseIDs).ToNot(BeEmpty(), fmt.Sprintf("Invalid ImageID in results of task create-pyxis-image. taskrun results: %+v", trAdditionalReleasePr.Status.TaskRunStatusFields.Results[0]))

				Expect(trReleaseImageIDs).ToNot(HaveLen(len(trAdditionalReleaseIDs)), "the number of image IDs should not be the same in both taskrun results. (%+v vs. %+v)", trReleasePr.Status.TaskRunStatusFields.Results[0], trAdditionalReleasePr.Status.TaskRunStatusFields.Results[0])

				if len(trReleaseImageIDs) > len(trAdditionalReleaseIDs) {
					imageIDs = trReleaseImageIDs
				} else {
					imageIDs = trAdditionalReleaseIDs
				}

				return imageIDs
			}, avgControllerQueryTimeout, releasecommon.DefaultInterval).Should(HaveLen(2))
		})

		It("tests that associated Release CR has completed for each Component's Snapshot", func() {
			Eventually(func() error {
				var errMsg string
				for _, cr := range []*releaseApi.Release{releaseCR1, releaseCR2} {
					cr, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", cr.Spec.Snapshot, devNamespace)
					Expect(err).ShouldNot(HaveOccurred())
					if !cr.IsReleased() {
						errMsg += fmt.Sprintf("release %s/%s is not marked as finished yet", cr.GetNamespace(), cr.GetName())
					}
				}
				if len(errMsg) > 1 {
					return fmt.Errorf(errMsg)
				}
				return nil
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Release CRs to be created in %s namespace", devNamespace)
		})

		It("validates that imageIds from task create-pyxis-image exist in Pyxis.", func() {
			for _, imageID := range imageIDs {
				Eventually(func() error {
					body, err := fw.AsKubeAdmin.ReleaseController.GetPyxisImageByImageID(releasecommon.PyxisStageImagesApiEndpoint, imageID,
						[]byte(pyxisCertDecoded), []byte(pyxisKeyDecoded))
					Expect(err).NotTo(HaveOccurred(), "failed to get response body")

					sbomImage := release.Image{}
					Expect(json.Unmarshal(body, &sbomImage)).To(Succeed(), "failed to unmarshal body content: %s", string(body))

					return nil
				}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed())
			}
		})
	})
})
