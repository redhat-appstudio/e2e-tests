package pipelines

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"strings"
	"regexp"

	"github.com/devfile/library/v2/pkg/util"
	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/has"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/tekton"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	releaseapi "github.com/redhat-appstudio/release-service/api/v1alpha1"
	releasecommon "github.com/redhat-appstudio/e2e-tests/tests/release"
	tektonutils "github.com/redhat-appstudio/release-service/tekton/utils"
	"k8s.io/apimachinery/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	advsServiceAccountName   = "release-service-account"
	advsCatalogPathInRepo    = "pipelines/rh-advisories/rh-advisories.yaml"
)

var component *appservice.Component

var _ = framework.ReleasePipelinesSuiteDescribe("e2e tests for rh-advisories pipeline", Label("release-pipelines", "rh-advisories"), func() {
	defer GinkgoRecover()
	var pyxisKeyDecoded, pyxisCertDecoded []byte

	var devWorkspace = utils.GetEnv(constants.RELEASE_DEV_WORKSPACE_ENV, constants.DevReleaseTeam)
	var managedWorkspace = utils.GetEnv(constants.RELEASE_MANAGED_WORKSPACE_ENV, constants.ManagedReleaseTeam)

	var devNamespace = devWorkspace + "-tenant"
	var managedNamespace = managedWorkspace + "-tenant"

	var err error
	var devFw *framework.Framework
	var managedFw *framework.Framework
	var advsApplicationName = "advs-app-" + util.GenerateRandomString(4)
	var advsComponentName = "advs-comp-" + util.GenerateRandomString(4)
	var advsReleasePlanName = "advs-rp-" + util.GenerateRandomString(4)
	var advsReleasePlanAdmissionName = "advs-rpa-" + util.GenerateRandomString(4)
	var advsEnterpriseContractPolicyName = "advs-policy-" + util.GenerateRandomString(4)

	var snapshot *appservice.Snapshot
	var releaseCR *releaseapi.Release
	var releasePR, buildPR *tektonv1.PipelineRun

	AfterEach(framework.ReportFailure(&devFw))

	Describe("Rh-advisories happy path", Label("rhAdvisories"), func() {
		BeforeAll(func() {
			devFw = releasecommon.NewFramework(devWorkspace)
			managedFw = releasecommon.NewFramework(managedWorkspace)
			managedNamespace = managedFw.UserNamespace

			sourceAuthJson := utils.GetEnv("QUAY_TOKEN", "")
			Expect(sourceAuthJson).ToNot(BeEmpty())
			// Create secret for the release registry repo "hacbs-release-tests".
			_, err = managedFw.AsKubeAdmin.CommonController.CreateRegistryAuthSecret(releasecommon.RedhatAppstudioUserSecret, managedNamespace, sourceAuthJson)
			//Expect(err).ToNot(HaveOccurred())
			// Linking the build secret to the pipeline service account in dev namespace.
			err = devFw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(devNamespace, releasecommon.HacbsReleaseTestsTokenSecret, constants.DefaultPipelineServiceAccount, true)
			Expect(err).ToNot(HaveOccurred())
			err = managedFw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.RedhatAppstudioUserSecret, releasecommon.ReleasePipelineServiceAccountDefault, true)
			//Expect(err).ToNot(HaveOccurred())

			keyPyxisStage := os.Getenv(constants.PYXIS_STAGE_KEY_ENV)
			Expect(keyPyxisStage).ToNot(BeEmpty())

			certPyxisStage := os.Getenv(constants.PYXIS_STAGE_CERT_ENV)
			Expect(certPyxisStage).ToNot(BeEmpty())

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

			// Delete the secret if it exists
			_ = managedFw.AsKubeAdmin.CommonController.DeleteSecret(managedNamespace, "pyxis")
			_, err = managedFw.AsKubeAdmin.CommonController.CreateSecret(managedNamespace, secret)
			Expect(err).ToNot(HaveOccurred())

			err = managedFw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.RedhatAppstudioUserSecret, constants.DefaultPipelineServiceAccount, true)
			Expect(err).ToNot(HaveOccurred())

			_, err = devFw.AsKubeDeveloper.HasController.CreateApplication(advsApplicationName, devNamespace)
			Expect(err).NotTo(HaveOccurred())

			createADVSReleasePlan(advsReleasePlanName, *devFw, devNamespace, advsApplicationName, managedNamespace, "true")
			component = releasecommon.CreateComponentByCDQ(*devFw, devNamespace, managedNamespace, advsApplicationName, advsComponentName, releasecommon.AdditionalGitSourceComponentUrl)
			createADVSReleasePlanAdmission(advsReleasePlanAdmissionName, *managedFw, devNamespace, managedNamespace, advsApplicationName, advsEnterpriseContractPolicyName, advsCatalogPathInRepo)

			createADVSEnterpriseContractPolicy(advsEnterpriseContractPolicyName, *managedFw, devNamespace, managedNamespace)
		})

		AfterAll(func() {
			devFw = releasecommon.NewFramework(devWorkspace)
			managedFw = releasecommon.NewFramework(managedWorkspace)
			Expect(devFw.AsKubeDeveloper.HasController.DeleteApplication(advsApplicationName, devNamespace, false)).NotTo(HaveOccurred())
			Expect(managedFw.AsKubeDeveloper.TektonController.DeleteEnterpriseContractPolicy(advsEnterpriseContractPolicyName, managedNamespace, false)).NotTo(HaveOccurred())
			Expect(managedFw.AsKubeDeveloper.ReleaseController.DeleteReleasePlanAdmission(advsReleasePlanAdmissionName, managedNamespace, false)).NotTo(HaveOccurred())
		})

		var _ = Describe("Post-release verification", func() {
			It("verifies that a build PipelineRun is created in dev namespace and succeeds", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				Eventually(func() error {
					buildPR, err = devFw.AsKubeDeveloper.HasController.GetComponentPipelineRun(component.Name, advsApplicationName, devNamespace, "")
					if err != nil {
						return err
					}
					if !buildPR.IsDone() {
						return fmt.Errorf("build pipelinerun %s in namespace %s did not finish yet", buildPR.Name, buildPR.Namespace)
					}
					Expect(tekton.HasPipelineRunSucceeded(buildPR)).To(BeTrue(), fmt.Sprintf("build pipelinerun %s/%s did not succeed", buildPR.GetNamespace(), buildPR.GetName()))
					snapshot, err = devFw.AsKubeDeveloper.IntegrationController.GetSnapshot("", buildPR.Name, "", devNamespace)
					if err != nil {
						return err
					}
					return nil
				}, releasecommon.BuildPipelineRunCompletionTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out when waiting for build pipelinerun to be created")
				Expect(devFw.AsKubeDeveloper.HasController.WaitForComponentPipelineToBeFinished(component, "", devFw.AsKubeDeveloper.TektonController, &has.RetryOptions{Retries: 3, Always: true})).To(Succeed())
			})
			It("verifies the advs release pipelinerun is running and succeeds", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				managedFw = releasecommon.NewFramework(managedWorkspace)

				releaseCR, err = devFw.AsKubeDeveloper.ReleaseController.GetRelease("", snapshot.Name, devNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				Eventually(func() error {
					releasePR, err = managedFw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedFw.UserNamespace, releaseCR.GetName(), releaseCR.GetNamespace())
					if err != nil {
						return err
					}
					GinkgoWriter.Println("Release PR: ", releasePR.Name)
					if  !releasePR.IsDone(){
						return fmt.Errorf("release pipelinerun %s in namespace %s did not finished yet", releasePR.Name, releasePR.Namespace)
					}
					return nil
				}, releasecommon.ReleasePipelineRunCompletionTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out when waiting for release pipelinerun to succeed")
				Expect(tekton.HasPipelineRunSucceeded(releasePR)).To(BeTrue(), fmt.Sprintf("release pipelinerun %s/%s did not succeed", releasePR.GetNamespace(), releasePR.GetName()))
			})

			It("verifies release CR completed and set succeeded.", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				Eventually(func() error {
					releaseCr, err := devFw.AsKubeDeveloper.ReleaseController.GetRelease("", snapshot.Name, devNamespace)
					if err != nil {
						return err
					}
					GinkgoWriter.Println("Release CR: ", releaseCr.Name)
					if !releaseCr.IsReleased() {
						return fmt.Errorf("release %s/%s is not marked as finished yet", releaseCR.GetNamespace(), releaseCR.GetName())
					}
					return nil
				}, 10 * time.Minute, releasecommon.DefaultInterval).Should(Succeed())
			})

			It("verifies if the repository URL is valid", func() {
				managedFw = releasecommon.NewFramework(managedWorkspace)
				trReleasePr, err := managedFw.AsKubeAdmin.TektonController.GetTaskRunStatus(managedFw.AsKubeAdmin.CommonController.KubeRest(), releasePR, "create-advisory")
				Expect(err).NotTo(HaveOccurred())
				trReleaseURL := trReleasePr.Status.TaskRunStatusFields.Results[0].Value.StringVal

				prefix := "https://gitlab.cee.redhat.com/rhtap-release/advisories"
				advisoryURL := strings.TrimPrefix(trReleaseURL, prefix)

				//pattern := `\-\/blob\/main\/data\/advisories\/[^\/]+\/[^\/]+\/[^\/]+\/advisory\.yaml`
				pattern := `\-\/blob\/main\/data\/advisories\/[^\/]+\/\d{4}\/[^\/]+\/advisory\.yaml`
				re, err := regexp.Compile(pattern)
				Expect(err).NotTo(HaveOccurred())

				Expect(re.MatchString(advisoryURL)).To(BeTrue(), fmt.Sprintf("Advisory_url %s is not valid", advisoryURL))
			})
		})
	})
})

func createADVSEnterpriseContractPolicy(advsECPName string, managedFw framework.Framework, devNamespace, managedNamespace string) {
	defaultEcPolicySpec := ecp.EnterpriseContractPolicySpec{
		Description: "Red Hat's enterprise requirements",
		PublicKey:   "k8s://openshift-pipelines/public-key",
		Sources: []ecp.Source{{
			Name:   "Default",
			Policy: []string{releasecommon.EcPolicyLibPath, releasecommon.EcPolicyReleasePath},
			Data:   []string{releasecommon.EcPolicyDataBundle, releasecommon.EcPolicyDataPath},
		}},
		Configuration: &ecp.EnterpriseContractPolicyConfiguration{
			Exclude: []string{"cve", "step_image_registries", "tasks.required_tasks_found:prefetch-dependencies"},
			Include: []string{"minimal"},
		},
	}

	_, err := managedFw.AsKubeDeveloper.TektonController.CreateEnterpriseContractPolicy(advsECPName, managedNamespace, defaultEcPolicySpec)
	Expect(err).NotTo(HaveOccurred())

}

func createADVSReleasePlan(advsReleasePlanName string, devFw framework.Framework, devNamespace, advsAppName, managedNamespace string, autoRelease string) {
	var err error

	data, err := json.Marshal(map[string]interface{}{
		"releaseNotes": map[string]interface{}{
			"description": "releaseNotes description",
			"references": []string{"https://server.com/ref1", "http://server2.com/ref2"},
			"solution": "some solution",
			"synopsis": "test synopsis",
			"topic": "test topic",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = devFw.AsKubeDeveloper.ReleaseController.CreateReleasePlan(advsReleasePlanName, devNamespace, advsAppName,
			managedNamespace, autoRelease, &runtime.RawExtension{
			Raw: data,
		})
	Expect(err).NotTo(HaveOccurred())
}

func createADVSReleasePlanAdmission(advsRPAName string, managedFw framework.Framework, devNamespace, managedNamespace, advsAppName, advsECPName, pathInRepoValue string) {
	var err error

	data, err := json.Marshal(map[string]interface{}{
		"mapping": map[string]interface{}{
			"components": []map[string]interface{}{
				{
					"name":       component.GetName(),
					"repository": releasecommon.AdditionalReleasedImagePushRepo,
				},
			},
		},
		"pyxis": map[string]interface{}{
			"server": "stage",
			"secret": "pyxis",
		},
		"releaseNotes": map[string]interface{}{
			"cpe": "cpe:/a:example.com",
			"product_id": "555",
			"product_name": "test product",
			"product_stream" : "rhtas-tp1",
			"product_version" : "v1.0",
			"type": "RHSA",
		},
		"sign": map[string]interface{}{
			"configMapName": "hacbs-signing-pipeline-config-redhatbeta2",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = managedFw.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission(advsRPAName, managedNamespace, "", devNamespace, advsECPName, advsServiceAccountName, []string{advsAppName}, true, &tektonutils.PipelineRef{
		Resolver: "git",
		Params: []tektonutils.Param{
			{Name: "url", Value: releasecommon.RelSvcCatalogURL},
			{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
			{Name: "pathInRepo", Value: pathInRepoValue},
		},
	}, &runtime.RawExtension{
		Raw: data,
	})
	Expect(err).NotTo(HaveOccurred())
}
