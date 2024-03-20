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

var _ = framework.ReleasePipelinesSuiteDescribe("e2e tests for rhtap-service-push pipeline", Label("release-pipelines", "rhtap-service-push", "HACBS"), func() {
	defer GinkgoRecover()
	// Initialize the tests controllers
	var fw *framework.Framework
	AfterEach(framework.ReportFailure(&fw))
	var err error
	var devNamespace, managedNamespace, compName string
	var avgControllerQueryTimeout = 5 * time.Minute

	var imageIDs []string
	var pyxisKeyDecoded, pyxisCertDecoded []byte
	var releasePR *pipeline.PipelineRun
	scGitRevision := fmt.Sprintf("test-rhtap-%s", util.GenerateRandomString(4))

	var component *appservice.Component
	var snapshot *appservice.Snapshot
	var releaseCR *releaseApi.Release

	var componentDetected appservice.ComponentDetectionDescription

	BeforeAll(func() {
		fw, err = framework.NewFramework(utils.GetGeneratedNamespace("rhtap-push"))
		Expect(err).NotTo(HaveOccurred())
		devNamespace = fw.UserNamespace
		managedNamespace = utils.GetGeneratedNamespace("rhtap-push-managed")

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

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(releasecommon.SourceReleasePlanName, devNamespace, releasecommon.ApplicationNameDefault, managedNamespace, "true")
		Expect(err).NotTo(HaveOccurred())

		data, err := json.Marshal(map[string]interface{}{
			"mapping": map[string]interface{}{
				"components": []map[string]interface{}{
					{
						"name":       compName,
						"repository": "quay.io/" + utils.GetQuayIOOrganization() + "/dcmetromap",
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
				{Name: "pathInRepo", Value: "pipelines/rhtap-service-push/rhtap-service-push.yaml"},
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

		It("verifies that Component can be created and build PipelineRun is created for it in dev namespace and succeeds", func() {
			component, err = fw.AsKubeAdmin.HasController.CreateComponent(componentDetected.ComponentStub, devNamespace, "", "", releasecommon.ApplicationNameDefault, true, map[string]string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "",
				fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
		})

		It("tests that Snapshot is created for the Component", func() {
			Eventually(func() error {
				snapshot, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", "", component.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Snapshot for component %s/%s: %v\n", component.GetNamespace(), component.GetName(), err)
					return err
				}
				return nil
			}, 5*time.Minute, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Snapshots to be created in %s namespace", devNamespace)
		})

		It("tests that associated Release CR is created for the Component's Snapshot", func() {
			Eventually(func() error {
				releaseCR, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", snapshot.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Release CR for snapshot %s/%s: %v\n", snapshot.GetNamespace(), component.GetName(), err)
					return err
				}
				return nil
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Release CRs to be created in %s namespace", devNamespace)
		})

		It("verifies that Release PipelineRun is triggered for the Release CR", func() {
			Eventually(func() error {
				releasePR, err = fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseCR.GetName(), releaseCR.GetNamespace())
				if err != nil {
					GinkgoWriter.Printf("release pipelineRun for Release %s/%s not created yet: %+v\n", releaseCR.GetNamespace(), releaseCR.GetName(), err)
					return err
				}
				var errMsg string
				Expect(tekton.HasPipelineRunFailed(releasePR)).ToNot(BeTrue(), fmt.Sprintf("Release PipelineRun %s/%s failed", releasePR.GetNamespace(), releasePR.GetName()))
				if !releasePR.HasStarted() {
					errMsg = fmt.Sprintf("Release PipelineRun %s/%s did not started yet\n", releasePR.GetNamespace(), releasePR.GetName())
				}
				if len(errMsg) > 1 {
					return fmt.Errorf(errMsg)
				}
				return nil
			}, releasecommon.ReleasePipelineRunCreationTimeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out waiting for a PipelineRun to start for each Release CR in %s namespace", managedNamespace))
		})

		It("verifies a release PipelineRun for the component succeeded in managed namespace", func() {
			Eventually(func() error {
				var errMsg string
				releasePR, err = fw.AsKubeAdmin.TektonController.GetPipelineRun(releasePR.GetName(), releasePR.GetNamespace())
				if err != nil {
					return err
				}
				Expect(tekton.HasPipelineRunFailed(releasePR)).ToNot(BeTrue(), fmt.Sprintf("Release PipelineRun %s/%s failed", releasePR.GetNamespace(), releasePR.GetName()))
				if releasePR.IsDone() {
					Expect(tekton.HasPipelineRunSucceeded(releasePR)).To(BeTrue(), fmt.Sprintf("Release PipelineRun %s/%s did not succceed", releasePR.GetNamespace(), releasePR.GetName()))
				} else {
					errMsg = fmt.Sprintf("Release PipelineRun %s/%s did not finish yet\n", releasePR.GetNamespace(), releasePR.GetName())
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

				releasePR, err = fw.AsKubeAdmin.TektonController.GetPipelineRun(releasePR.GetName(), releasePR.GetNamespace())
				Expect(err).NotTo(HaveOccurred())

				trReleasePr, err := fw.AsKubeAdmin.TektonController.GetTaskRunStatus(fw.AsKubeAdmin.CommonController.KubeRest(), releasePR, "create-pyxis-image")
				Expect(err).NotTo(HaveOccurred())

				trReleaseImageIDs := re.FindAllString(trReleasePr.Status.TaskRunStatusFields.Results[0].Value.StringVal, -1)

				Expect(trReleaseImageIDs).ToNot(BeEmpty(), fmt.Sprintf("Invalid ImageID in results of task create-pyxis-image. taskrun results: %+v", trReleasePr.Status.TaskRunStatusFields.Results[0]))

				imageIDs = trReleaseImageIDs

				return imageIDs
			}, avgControllerQueryTimeout, releasecommon.DefaultInterval).Should(HaveLen(2))
		})

		It("tests that associated Release CR has completed for each Component's Snapshot", func() {
			Eventually(func() error {
				var errMsg string
				releaseCR, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", releaseCR.Spec.Snapshot, devNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				if !releaseCR.IsReleased() {
					errMsg = fmt.Sprintf("release %s/%s is not marked as finished yet", releaseCR.GetNamespace(), releaseCR.GetName())
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

					if sbomImage.ContentManifest.ID == "" {
						return fmt.Errorf("ContentManifest field is empty for sbom image: %+v", sbomImage)
					}

					return nil
				}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed())
			}
		})
	})
})
