package pipelines

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"strings"

	"github.com/devfile/library/v2/pkg/util"
	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/clients/github"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	"github.com/konflux-ci/e2e-tests/pkg/utils/tekton"
	releasecommon "github.com/konflux-ci/e2e-tests/tests/release"
	releaseapi "github.com/konflux-ci/release-service/api/v1alpha1"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
)

const (
	rhtapServiceAccountName  = "release-service-account"
	rhtapCatalogPathInRepo   = "pipelines/rhtap-service-push/rhtap-service-push.yaml"
	rhtapGitSourceURL        = "https://github.com/redhat-appstudio-qe/devfile-sample-python-basic-test2"
	rhtapGitSourceRepoName   = "devfile-sample-python-basic-test2"
	rhtapGitSrcDefaultSHA    = "47fc22092005aabebce233a9b6eab994a8152bbd"
	rhtapGitSrcDefaultBranch = "main"
)

var rhtapComponent *appservice.Component
var gh *github.Github

var _ = framework.ReleasePipelinesSuiteDescribe("e2e tests for rhtap-service-push pipeline", Label("release-pipelines", "rhtap-service-push"), func() {
	defer GinkgoRecover()
	var pyxisKeyDecoded, pyxisCertDecoded []byte

	var devWorkspace = utils.GetEnv(constants.RELEASE_DEV_WORKSPACE_ENV, constants.DevReleaseTeam)
	var managedWorkspace = utils.GetEnv(constants.RELEASE_MANAGED_WORKSPACE_ENV, constants.ManagedReleaseTeam)

	var devNamespace = devWorkspace + "-tenant"
	var managedNamespace = managedWorkspace + "-tenant"

	var err error
	var devFw *framework.Framework
	var managedFw *framework.Framework
	var rhtapApplicationName = "rhtap-app-" + util.GenerateRandomString(4)
	var rhtapComponentName = "rhtap-comp-" + util.GenerateRandomString(4)
	var rhtapReleasePlanName = "rhtap-rp-" + util.GenerateRandomString(4)
	var rhtapReleasePlanAdmissionName = "rhtap-rpa-" + util.GenerateRandomString(4)
	var rhtapEnterpriseContractPolicyName = "rhtap-policy-" + util.GenerateRandomString(4)
	//Branch for creating pull request
	var testPRBranchName = fmt.Sprintf("%s-%s", "pr-branch", util.GenerateRandomString(6))
	var testBaseBranchName = fmt.Sprintf("%s-%s", "base-branch", util.GenerateRandomString(6))
	var mergeResultSha string
	var sourcePrNum int

	var snapshot *appservice.Snapshot
	var releaseCR *releaseapi.Release
	var buildPR *tektonv1.PipelineRun

	AfterEach(framework.ReportFailure(&devFw))

	Describe("Rhtap-service-push happy path", Label("RhtapServicePush"), func() {
		BeforeAll(func() {
			devFw = releasecommon.NewFramework(devWorkspace)
			managedFw = releasecommon.NewFramework(managedWorkspace)
			managedNamespace = managedFw.UserNamespace

			githubUser := "hacbs-release"
			githubToken := utils.GetEnv(constants.GITHUB_TOKEN_ENV, "")
			gh, err = github.NewGithubClient(githubToken, githubUser)
			Expect(githubToken).ToNot(BeEmpty())

			err = devFw.AsKubeAdmin.CommonController.Github.CreateRef(rhtapGitSourceRepoName, rhtapGitSrcDefaultBranch, rhtapGitSrcDefaultSHA, testBaseBranchName)
                        Expect(err).ShouldNot(HaveOccurred())

			//Create the ref, add the file,  create the PR and merge the PR
			err = devFw.AsKubeAdmin.CommonController.Github.CreateRef(rhtapGitSourceRepoName, rhtapGitSrcDefaultBranch, rhtapGitSrcDefaultSHA, testPRBranchName)
			Expect(err).ShouldNot(HaveOccurred())
			fileToCreatePath := fmt.Sprintf("%s/sample-file.txt", "test")
			createdFileSha, err := devFw.AsKubeAdmin.CommonController.Github.CreateFile(rhtapGitSourceRepoName, fileToCreatePath, fmt.Sprintf("sample test file inside %s", "test"), testPRBranchName)
			Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("error while creating file: %s", fileToCreatePath))
			pr, err := devFw.AsKubeAdmin.CommonController.Github.CreatePullRequest(rhtapGitSourceRepoName, "sample pr title", "sample pr body", testPRBranchName, testBaseBranchName)
			Expect(err).ShouldNot(HaveOccurred())
			GinkgoWriter.Printf("PR #%d got created with sha %s\n", pr.GetNumber(), createdFileSha.GetSHA())
			sourcePrNum = pr.GetNumber()
			Eventually(func() error {
				mergeResult, err := devFw.AsKubeAdmin.CommonController.Github.MergePullRequest(rhtapGitSourceRepoName, pr.GetNumber())
				if err == nil {
					mergeResultSha = mergeResult.GetSHA()
					GinkgoWriter.Printf("merged result sha: %s for PR #%d\n", mergeResultSha, pr.GetNumber())
					return nil
				}
				return fmt.Errorf("PR merge failed")
			}, 10*time.Minute, releasecommon.DefaultInterval).Should(Succeed())

			keyPyxisStage := os.Getenv(constants.PYXIS_STAGE_KEY_ENV)
			Expect(keyPyxisStage).ToNot(BeEmpty())

			certPyxisStage := os.Getenv(constants.PYXIS_STAGE_CERT_ENV)
			Expect(certPyxisStage).ToNot(BeEmpty())

			// Creating k8s secret to access Pyxis stage based on base64 decoded of key and cert
			pyxisKeyDecoded, err = base64.StdEncoding.DecodeString(string(keyPyxisStage))
			Expect(err).ToNot(HaveOccurred())

			pyxisCertDecoded, err = base64.StdEncoding.DecodeString(string(certPyxisStage))
			Expect(err).ToNot(HaveOccurred())

			_, err = managedFw.AsKubeAdmin.CommonController.GetSecret(managedNamespace, releasecommon.RedhatAppstudioQESecret)
			if errors.IsNotFound(err) {
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

				_, err = managedFw.AsKubeAdmin.CommonController.CreateSecret(managedNamespace, secret)
				Expect(err).ToNot(HaveOccurred())
			}

			err = managedFw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.RedhatAppstudioUserSecret, constants.DefaultPipelineServiceAccount, true)
			Expect(err).ToNot(HaveOccurred())

			_, err = devFw.AsKubeDeveloper.HasController.CreateApplication(rhtapApplicationName, devNamespace)
			Expect(err).NotTo(HaveOccurred())

			_, err = devFw.AsKubeDeveloper.ReleaseController.CreateReleasePlan(rhtapReleasePlanName, devNamespace, rhtapApplicationName, managedNamespace, "true", nil)
			Expect(err).NotTo(HaveOccurred())

			componentObj := appstudioApi.ComponentSpec{
				ComponentName: rhtapComponentName,
				Application:   rhtapApplicationName,
				Source: appstudioApi.ComponentSource{
					ComponentSourceUnion: appstudioApi.ComponentSourceUnion{
						GitSource: &appstudioApi.GitSource{
							URL:      rhtapGitSourceURL,
							Revision: testBaseBranchName,
							Context:  ".",
							DockerfileURL: constants.DockerFilePath,
						},
					},
				},
			}
			// Create a component with Git Source URL, a specified git branch
			rhtapComponent, err = devFw.AsKubeAdmin.HasController.CreateComponent(componentObj, devNamespace, "", "", rhtapApplicationName, false, constants.DefaultDockerBuildPipelineBundle)
			Expect(err).ShouldNot(HaveOccurred())

			createRHTAPReleasePlanAdmission(rhtapReleasePlanAdmissionName, *managedFw, devNamespace, managedNamespace, rhtapApplicationName, rhtapEnterpriseContractPolicyName, rhtapCatalogPathInRepo)

			createRHTAPEnterpriseContractPolicy(rhtapEnterpriseContractPolicyName, *managedFw, devNamespace, managedNamespace)
		})

		AfterAll(func() {
			devFw = releasecommon.NewFramework(devWorkspace)
			managedFw = releasecommon.NewFramework(managedWorkspace)
			Expect(devFw.AsKubeDeveloper.HasController.DeleteApplication(rhtapApplicationName, devNamespace, false)).NotTo(HaveOccurred())
			Expect(managedFw.AsKubeDeveloper.TektonController.DeleteEnterpriseContractPolicy(rhtapEnterpriseContractPolicyName, managedNamespace, false)).NotTo(HaveOccurred())
			Expect(managedFw.AsKubeDeveloper.ReleaseController.DeleteReleasePlanAdmission(rhtapReleasePlanAdmissionName, managedNamespace, false)).NotTo(HaveOccurred())
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
					buildPR, err = devFw.AsKubeDeveloper.HasController.GetComponentPipelineRun(rhtapComponent.Name, rhtapApplicationName, devNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("Build PipelineRun has not been created yet for the component %s/%s\n", devNamespace, rhtapComponent.Name)
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
				}, releasecommon.BuildPipelineRunCompletionTimeout, releasecommon.DefaultInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the build PipelineRun to be finished for the component %s/%s", devNamespace, rhtapComponent.Name))
			})
			It("verifies the rhtap release pipelinerun is running and succeeds", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				managedFw = releasecommon.NewFramework(managedWorkspace)

				releaseCR, err = devFw.AsKubeDeveloper.ReleaseController.GetRelease("", snapshot.Name, devNamespace)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(managedFw.AsKubeAdmin.ReleaseController.WaitForReleasePipelineToBeFinished(releaseCR, managedNamespace)).To(Succeed(), fmt.Sprintf("Error when waiting for a release pipelinerun for release %s/%s to finish", releaseCR.GetNamespace(), releaseCR.GetName()))
			})

			It("verifies release CR completed and set succeeded.", func() {
				devFw = releasecommon.NewFramework(devWorkspace)
				Eventually(func() error {
					releaseCR, err = devFw.AsKubeDeveloper.ReleaseController.GetRelease("", snapshot.Name, devNamespace)
					if err != nil {
						return err
					}
					GinkgoWriter.Println("Release CR: ", releaseCR.Name)
					if !releaseCR.IsReleased() {
						return fmt.Errorf("release %s/%s is not marked as finished yet", releaseCR.GetNamespace(), releaseCR.GetName())
					}
					return nil
				}, 10*time.Minute, releasecommon.DefaultInterval).Should(Succeed())
			})
			It("verifies if the PR in infra-deployments repo is created/updated", func() {
				Eventually(func() error {
					prs, err := gh.ListPullRequests("infra-deployments")
					Expect(err).ShouldNot(HaveOccurred())
					for _, pr := range prs {
						GinkgoWriter.Printf("PR branch: %s", pr.Head.GetRef())
						if strings.Contains(pr.Head.GetRef(), rhtapGitSourceRepoName) {
							contents, err := gh.GetFile("infra-deployments", "components/release/development/kustomization.yaml", rhtapGitSourceRepoName)
							Expect(err).ShouldNot(HaveOccurred())
							content, err := contents.GetContent()
							Expect(err).ShouldNot(HaveOccurred())
							GinkgoWriter.Printf("Content of PR #%d: %s \n", pr.GetNumber(), content)
							if strings.Contains(content, mergeResultSha) {
								GinkgoWriter.Printf("The reference is updated!")
								return nil
							}

							body := pr.Body
							GinkgoWriter.Printf("Body of PR #%d: %s \n", pr.GetNumber(), *body)
							prLink := fmt.Sprintf(rhtapGitSourceURL + "/pull/%d", sourcePrNum)
							GinkgoWriter.Printf("The source PR link: %s", prLink)
							if strings.Contains(*body, prLink) {
								GinkgoWriter.Printf("Added source PR%d to the PR of infra-deployments!", sourcePrNum)
								return nil
							}
						}
					}
					return fmt.Errorf("The image SHA is not updated and the PR is not found after update-infra-deploymentst")
				}, 10*time.Minute, releasecommon.DefaultInterval).Should(Succeed())
			})
		})
	})
})

func createRHTAPEnterpriseContractPolicy(rhtapECPName string, managedFw framework.Framework, devNamespace, managedNamespace string) {
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

	_, err := managedFw.AsKubeDeveloper.TektonController.CreateEnterpriseContractPolicy(rhtapECPName, managedNamespace, defaultEcPolicySpec)
	Expect(err).NotTo(HaveOccurred())

}

func createRHTAPReleasePlanAdmission(rhtapRPAName string, managedFw framework.Framework, devNamespace, managedNamespace, rhtapAppName, rhtapECPName, pathInRepoValue string) {
	var err error

	data, err := json.Marshal(map[string]interface{}{
		"mapping": map[string]interface{}{
			"components": []map[string]interface{}{
				{
					"name":       rhtapComponent.GetName(),
					"repository": "quay.io/redhat-pending/rhtap----konflux-release-e2e",
					"tags": []string{"latest", "latest-{{ timestamp }}", "testtag",
						"testtag-{{ timestamp }}", "testtag2", "testtag2-{{ timestamp }}"},
					"source": map[string]interface{}{
						"git": map[string]interface{}{
							"url": rhtapGitSourceURL,
						},
					},
				},
			},
		},
		"pyxis": map[string]interface{}{
			"server": "stage",
			"secret": "pyxis",
		},
		"targetGHRepo": "hacbs-release/infra-deployments",
		"infra-deployment-update-script": "sed -i -e 's|\\(https://github.com/hacbs-release/release-service/config/default?ref=\\)\\(.*\\)|\\1{{ revision }}|' -e 's/\\(newTag: \\).*/\\1{{ revision }}/' components/release/development/kustomization.yaml",
		"sign": map[string]interface{}{
			"configMapName": "hacbs-signing-pipeline-config-redhatbeta2",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = managedFw.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission(rhtapRPAName, managedNamespace, "", devNamespace, rhtapECPName, rhtapServiceAccountName, []string{rhtapAppName}, true, &tektonutils.PipelineRef{
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
