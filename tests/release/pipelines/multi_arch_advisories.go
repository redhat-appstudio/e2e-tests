package pipelines

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	"github.com/konflux-ci/e2e-tests/pkg/utils/tekton"
	releasecommon "github.com/konflux-ci/e2e-tests/tests/release"
	releaseapi "github.com/konflux-ci/release-service/api/v1alpha1"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
)

const (
	multiServiceAccountName = "release-service-account"
	multiCatalogPathInRepo  = "pipelines/rh-advisories/rh-advisories.yaml"
)

var mcomponent *appservice.Component

var _ = framework.ReleasePipelinesSuiteDescribe("e2e tests for rh-advisories pipeline", Label("release-pipelines", "multiarch-advisories"), func() {
	defer GinkgoRecover()
	var pyxisKeyDecoded, pyxisCertDecoded []byte

	var devWorkspace = utils.GetEnv(constants.RELEASE_DEV_WORKSPACE_ENV, constants.DevReleaseTeam)
	var managedWorkspace = utils.GetEnv(constants.RELEASE_MANAGED_WORKSPACE_ENV, constants.ManagedReleaseTeam)

	var devNamespace = devWorkspace + "-tenant"
	var managedNamespace = managedWorkspace + "-tenant"

	var err error
	var devFw *framework.Framework
	var managedFw *framework.Framework
	var multiApplicationName = "multi-app-" + util.GenerateRandomString(4)
	var multiComponentName = "multi-comp-" + util.GenerateRandomString(4)
	var multiReleasePlanName = "multi-rp-" + util.GenerateRandomString(4)
	var multiReleasePlanAdmissionName = "multi-rpa-" + util.GenerateRandomString(4)
	var multiEnterpriseContractPolicyName = "multi-policy-" + util.GenerateRandomString(4)

	var snapshot *appservice.Snapshot
	var releaseCR *releaseapi.Release
	var releasePR, buildPR *tektonv1.PipelineRun

	AfterEach(framework.ReportFailure(&devFw))

	Describe("Multiarch-advisories happy path", Label("multiArchAdvisories"), func() {
		BeforeAll(func() {
			devFw = releasecommon.NewFramework(devWorkspace)
			managedFw = releasecommon.NewFramework(managedWorkspace)
			managedNamespace = managedFw.UserNamespace

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

			// Delete the secret if it exists in case it is not correct
			_ = managedFw.AsKubeAdmin.CommonController.DeleteSecret(managedNamespace, "pyxis")
			_, err = managedFw.AsKubeAdmin.CommonController.CreateSecret(managedNamespace, secret)
			Expect(err).ToNot(HaveOccurred())

			err = managedFw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.RedhatAppstudioUserSecret, constants.DefaultPipelineServiceAccount, true)
			Expect(err).ToNot(HaveOccurred())

			_, err = devFw.AsKubeDeveloper.HasController.CreateApplication(multiApplicationName, devNamespace)
			Expect(err).NotTo(HaveOccurred())

			createMULTIReleasePlan(multiReleasePlanName, *devFw, devNamespace, multiApplicationName, managedNamespace, "true")

			customBuildahRemotePipeline := utils.GetEnv("CUSTOM_MULTI_ARCH_PIPELINE_BUILD_BUNDLE", "quay.io/redhat-appstudio-qe/test-images@sha256:f527c1a4e77559cc94ee32c9751fac36cf32883e6c2e28c0df0f4773d2c25f54")

			buildPipelineAnnotation := map[string]string{
				"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"multi-arch-pipeline", "bundle": "%s"}`, customBuildahRemotePipeline),
			}

			mcomponent = releasecommon.CreateComponent(*devFw, devNamespace, multiApplicationName, multiComponentName, releasecommon.MultiArchComponentUrl, "", ".", constants.DockerFilePath, buildPipelineAnnotation)

			createMULTIReleasePlanAdmission(multiReleasePlanAdmissionName, *managedFw, devNamespace, managedNamespace, multiApplicationName, multiEnterpriseContractPolicyName, multiCatalogPathInRepo)

			createMULTIEnterpriseContractPolicy(multiEnterpriseContractPolicyName, *managedFw, devNamespace, managedNamespace)
		})

		AfterAll(func() {
			devFw = releasecommon.NewFramework(devWorkspace)
			managedFw = releasecommon.NewFramework(managedWorkspace)
			Expect(devFw.AsKubeDeveloper.HasController.DeleteApplication(multiApplicationName, devNamespace, false)).NotTo(HaveOccurred())
			Expect(managedFw.AsKubeDeveloper.TektonController.DeleteEnterpriseContractPolicy(multiEnterpriseContractPolicyName, managedNamespace, false)).NotTo(HaveOccurred())
			Expect(managedFw.AsKubeDeveloper.ReleaseController.DeleteReleasePlanAdmission(multiReleasePlanAdmissionName, managedNamespace, false)).NotTo(HaveOccurred())
		})

		var _ = Describe("Post-release verification", func() {
			It("verifies that a build PipelineRun is created in dev namespace and succeeds", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				managedFw = releasecommon.NewFramework(managedWorkspace)
				// Create a ticker that ticks every 3 minutes
				ticker := time.NewTicker(3 * time.Minute)
				// Schedule the stop of the ticker after 15 minutes
				time.AfterFunc(15*time.Minute, func() {
					ticker.Stop()
					fmt.Println("Stopped executing every 3 minutes.")
				})
				// Run a goroutine to handle the ticker ticks
				go func() {
					for range ticker.C {
						devFw = releasecommon.NewFramework(devWorkspace)
						managedFw = releasecommon.NewFramework(managedWorkspace)
					}
				}()
				Eventually(func() error {
					buildPR, err = devFw.AsKubeDeveloper.HasController.GetComponentPipelineRun(mcomponent.Name, multiApplicationName, devNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("Build PipelineRun has not been created yet for the component %s/%s\n", devNamespace, mcomponent.Name)
						return err
					}
					GinkgoWriter.Printf("PipelineRun %s reason: %s\n", buildPR.Name, buildPR.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason())
					if !buildPR.IsDone() {
						return fmt.Errorf("build pipelinerun %s in namespace %s did not finish yet", buildPR.Name, buildPR.Namespace)
					}
					if buildPR.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsTrue() {
						snapshot, err = devFw.AsKubeDeveloper.IntegrationController.GetSnapshot("", buildPR.Name, "", devNamespace)
						if err != nil {
							return err
						}
						return nil
					} else {
						return fmt.Errorf(tekton.GetFailedPipelineRunLogs(devFw.AsKubeDeveloper.HasController.KubeRest(), devFw.AsKubeDeveloper.HasController.KubeInterface(), buildPR))
					}
				}, releasecommon.BuildPipelineRunCompletionTimeout, releasecommon.DefaultInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the build PipelineRun to be finished for the component %s/%s", devNamespace, mcomponent.Name))
			})
			It("verifies the multi release pipelinerun is running and succeeds", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				managedFw = releasecommon.NewFramework(managedWorkspace)

				releaseCR, err = devFw.AsKubeDeveloper.ReleaseController.GetRelease("", snapshot.Name, devNamespace)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(managedFw.AsKubeAdmin.ReleaseController.WaitForReleasePipelineToBeFinished(releaseCR, managedNamespace)).To(Succeed(), fmt.Sprintf("Error when waiting for a release pipelinerun for release %s/%s to finish", releaseCR.GetNamespace(), releaseCR.GetName()))
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
				}, 10*time.Minute, releasecommon.DefaultInterval).Should(Succeed())
			})

			It("verifies if the repository URL is valid", func() {
				managedFw = releasecommon.NewFramework(managedWorkspace)
				releasePR, err = managedFw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedFw.UserNamespace, releaseCR.GetName(), releaseCR.GetNamespace())
				Expect(err).NotTo(HaveOccurred())
				advisoryURL := releasePR.Status.PipelineRunStatusFields.Results[0].Value.StringVal
				pattern := `https?://[^/\s]+/[^/\s]+/[^/\s]+/+\-\/blob\/main\/data\/advisories\/[^\/]+\/[^\/]+\/[^\/]+\/advisory\.yaml`
				re, err := regexp.Compile(pattern)
				Expect(err).NotTo(HaveOccurred())
				Expect(re.MatchString(advisoryURL)).To(BeTrue(), fmt.Sprintf("Advisory_url %s is not valid", advisoryURL))
			})
		})
	})
})

func createMULTIEnterpriseContractPolicy(multiECPName string, managedFw framework.Framework, devNamespace, managedNamespace string) {
	defaultEcPolicySpec := ecp.EnterpriseContractPolicySpec{
		Description: "Red Hat's enterprise requirements",
		PublicKey:   "k8s://openshift-pipelines/public-key",
		Sources: []ecp.Source{{
			Name:   "Default",
			Policy: []string{releasecommon.EcPolicyLibPath, releasecommon.EcPolicyReleasePath},
			Data:   []string{releasecommon.EcPolicyDataBundle, releasecommon.EcPolicyDataPath},
		}},
		Configuration: &ecp.EnterpriseContractPolicyConfiguration{
			Exclude: []string{"step_image_registries", "tasks.required_tasks_found:prefetch-dependencies"},
			Include: []string{"@slsa3"},
		},
	}

	_, err := managedFw.AsKubeDeveloper.TektonController.CreateEnterpriseContractPolicy(multiECPName, managedNamespace, defaultEcPolicySpec)
	Expect(err).NotTo(HaveOccurred())

}

func createMULTIReleasePlan(multiReleasePlanName string, devFw framework.Framework, devNamespace, multiAppName, managedNamespace string, autoRelease string) {
	var err error

	data, err := json.Marshal(map[string]interface{}{
		"releaseNotes": map[string]interface{}{
			"description": "releaseNotes description",
			"references":  []string{"https://server.com/ref1", "http://server2.com/ref2"},
			"solution":    "some solution",
			"synopsis":    "test synopsis",
			"topic":       "test topic",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = devFw.AsKubeDeveloper.ReleaseController.CreateReleasePlan(multiReleasePlanName, devNamespace, multiAppName,
		managedNamespace, autoRelease, &runtime.RawExtension{
			Raw: data,
		})
	Expect(err).NotTo(HaveOccurred())
}

func createMULTIReleasePlanAdmission(multiRPAName string, managedFw framework.Framework, devNamespace, managedNamespace, multiAppName, multiECPName, pathInRepoValue string) {
	var err error

	data, err := json.Marshal(map[string]interface{}{
		"mapping": map[string]interface{}{
			"components": []map[string]interface{}{
				{
					"name":       mcomponent.GetName(),
					"repository": "quay.io/redhat-pending/rhtap----konflux-release-e2e",
					"tags": []string{"latest", "latest-{{ timestamp }}", "testtag",
						"testtag-{{ timestamp }}", "testtag2", "testtag2-{{ timestamp }}"},
				},
			},
		},
		"pyxis": map[string]interface{}{
			"server": "stage",
			"secret": "pyxis",
		},
		"releaseNotes": map[string]interface{}{
			"cpe":             "cpe:/a:example.com",
			"product_id":      "555",
			"product_name":    "test product",
			"product_stream":  "rhtas-tp1",
			"product_version": "v1.0",
			"type":            "RHSA",
		},
		"sign": map[string]interface{}{
			"configMapName": "hacbs-signing-pipeline-config-redhatbeta2",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = managedFw.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission(multiRPAName, managedNamespace, "", devNamespace, multiECPName, multiServiceAccountName, []string{multiAppName}, true, &tektonutils.PipelineRef{
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
