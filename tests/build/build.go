package build

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/google/go-github/v44/github"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	"github.com/konflux-ci/e2e-tests/pkg/utils/build"
	"github.com/konflux-ci/e2e-tests/pkg/utils/tekton"

	"github.com/devfile/library/v2/pkg/util"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/konflux-ci/build-service/controllers"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	releasecommon "github.com/konflux-ci/e2e-tests/tests/release"
	imagecontollers "github.com/konflux-ci/image-controller/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = framework.BuildSuiteDescribe("Build service E2E tests", Label("build", "HACBS"), func() {

	var f *framework.Framework
	AfterEach(framework.ReportFailure(&f))

	var err error
	defer GinkgoRecover()

	Describe("test PaC component build", Ordered, Label("github-webhook", "pac-build", "pipeline", "image-controller"), func() {
		var applicationName, componentName, componentBaseBranchName, pacBranchName, testNamespace, defaultBranchTestComponentName, imageRepoName, robotAccountName string
		var component *appservice.Component
		var plr *pipeline.PipelineRun

		var timeout, interval time.Duration

		var prNumber int
		var prHeadSha string

		BeforeAll(func() {
			if os.Getenv(constants.SKIP_PAC_TESTS_ENV) == "true" {
				Skip("Skipping this test due to configuration issue with Spray proxy")
			}

			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			if utils.IsPrivateHostname(f.OpenshiftConsoleHost) {
				Skip("Using private cluster (not reachable from Github), skipping...")
			}

			quayOrg := utils.GetEnv("DEFAULT_QUAY_ORG", "")
			supports, err := build.DoesQuayOrgSupportPrivateRepo()
			Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("error while checking if quay org supports private repo: %+v", err))
			if !supports {
				if quayOrg == "redhat-appstudio-qe" {
					Fail("Failed to create private image repo in redhat-appstudio-qe org")
				} else {
					Skip("Quay org does not support private quay repository creation, please add support for private repo creation before running this test")
				}
			}
			Expect(err).ShouldNot(HaveOccurred())

			applicationName = fmt.Sprintf("build-suite-test-application-%s", util.GenerateRandomString(4))
			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			componentName = fmt.Sprintf("%s-%s", "test-component-pac", util.GenerateRandomString(6))
			pacBranchName = constants.PaCPullRequestBranchPrefix + componentName
			componentBaseBranchName = fmt.Sprintf("base-%s", util.GenerateRandomString(6))

			err = f.AsKubeAdmin.CommonController.Github.CreateRef(helloWorldComponentGitSourceRepoName, helloWorldComponentDefaultBranch, helloWorldComponentRevision, componentBaseBranchName)
			Expect(err).ShouldNot(HaveOccurred())

			defaultBranchTestComponentName = fmt.Sprintf("test-custom-default-branch-%s", util.GenerateRandomString(6))
		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}

			// Delete new branches created by PaC and a testing branch used as a component's base branch
			err = f.AsKubeAdmin.CommonController.Github.DeleteRef(helloWorldComponentGitSourceRepoName, pacBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}
			err = f.AsKubeAdmin.CommonController.Github.DeleteRef(helloWorldComponentGitSourceRepoName, componentBaseBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}
			err = f.AsKubeAdmin.CommonController.Github.DeleteRef(helloWorldComponentGitSourceRepoName, constants.PaCPullRequestBranchPrefix+defaultBranchTestComponentName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}

			// Delete created webhook from GitHub
			cleanupWebhooks(f, helloWorldComponentGitSourceRepoName)

		})

		When("a new component without specified branch is created and with visibility private", Label("pac-custom-default-branch"), func() {
			BeforeAll(func() {
				componentObj := appservice.ComponentSpec{
					ComponentName: defaultBranchTestComponentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           helloWorldComponentGitSourceURL,
								Revision:      "",
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}
				_, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPrivateRepo), constants.DefaultDockerBuildPipelineBundle))
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("correctly targets the default branch (that is not named 'main') with PaC", func() {
				timeout = time.Second * 300
				interval = time.Second * 1
				Eventually(func() bool {
					prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(helloWorldComponentGitSourceRepoName)
					Expect(err).ShouldNot(HaveOccurred())

					for _, pr := range prs {
						if pr.Head.GetRef() == constants.PaCPullRequestBranchPrefix+defaultBranchTestComponentName {
							Expect(pr.GetBase().GetRef()).To(Equal(helloWorldComponentDefaultBranch))
							return true
						}
					}
					return false
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for init PaC PR to be created against %s branch in %s repository", helloWorldComponentDefaultBranch, helloWorldComponentGitSourceRepoName))
			})
			It("workspace parameter is set correctly in PaC repository CR", func() {
				nsObj, err := f.AsKubeAdmin.CommonController.GetNamespace(testNamespace)
				Expect(err).ShouldNot(HaveOccurred())
				wsName := nsObj.Labels["appstudio.redhat.com/workspace_name"]
				repositoryParams, err := f.AsKubeAdmin.TektonController.GetRepositoryParams(defaultBranchTestComponentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), "error while trying to get repository params")
				paramExists := false
				for _, param := range repositoryParams {
					if param.Name == "appstudio_workspace" {
						paramExists = true
						Expect(param.Value).To(Equal(wsName), fmt.Sprintf("got workspace param value: %s, expected %s", param.Value, wsName))
					}
				}
				Expect(paramExists).To(BeTrue(), "appstudio_workspace param does not exists in repository CR")

			})
			It("triggers a PipelineRun", func() {
				timeout = time.Minute * 5
				Eventually(func() error {
					plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(defaultBranchTestComponentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, defaultBranchTestComponentName)
						return err
					}
					if !plr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", plr.GetNamespace(), plr.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", defaultBranchTestComponentName, testNamespace))
			})
			It("component build status is set correctly", func() {
				var buildStatus *controllers.BuildStatus
				Eventually(func() (bool, error) {
					component, err := f.AsKubeAdmin.HasController.GetComponent(defaultBranchTestComponentName, testNamespace)
					if err != nil {
						return false, err
					}

					buildStatusAnnotationValue := component.Annotations[controllers.BuildStatusAnnotationName]
					GinkgoWriter.Printf(buildStatusAnnotationValueLoggingFormat, buildStatusAnnotationValue)
					statusBytes := []byte(buildStatusAnnotationValue)

					err = json.Unmarshal(statusBytes, &buildStatus)
					if err != nil {
						return false, err
					}

					if buildStatus.PaC != nil {
						GinkgoWriter.Printf("state: %s\n", buildStatus.PaC.State)
						GinkgoWriter.Printf("mergeUrl: %s\n", buildStatus.PaC.MergeUrl)
						GinkgoWriter.Printf("errId: %d\n", buildStatus.PaC.ErrId)
						GinkgoWriter.Printf("errMessage: %s\n", buildStatus.PaC.ErrMessage)
						GinkgoWriter.Printf("configurationTime: %s\n", buildStatus.PaC.ConfigurationTime)
					} else {
						GinkgoWriter.Println("build status does not have PaC field")
					}

					return buildStatus.PaC != nil && buildStatus.PaC.State == "enabled" && buildStatus.PaC.MergeUrl != "" && buildStatus.PaC.ErrId == 0 && buildStatus.PaC.ConfigurationTime != "", nil
				}, timeout, interval).Should(BeTrue(), "component build status has unexpected content")
			})
			It("image repo and robot account created successfully", func() {
				component, err := f.AsKubeAdmin.HasController.GetComponent(defaultBranchTestComponentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), "could not get component %s in the %s namespace", defaultBranchTestComponentName, testNamespace)

				annotations := component.GetAnnotations()
				imageRepoName, err = build.GetQuayImageName(annotations)
				Expect(err).ShouldNot(HaveOccurred(), "failed to read image repo name from %+v", annotations)

				imageExist, err := build.DoesImageRepoExistInQuay(imageRepoName)
				Expect(err).ShouldNot(HaveOccurred(), "failed while checking if image repo exists in quay with error: %+v", err)
				Expect(imageExist).To(BeTrue(), "quay image does not exists")

				robotAccountName = build.GetRobotAccountName(imageRepoName)
				robotAccountExist, err := build.DoesRobotAccountExistInQuay(robotAccountName)
				Expect(err).ShouldNot(HaveOccurred(), "failed while checking if robot account exists in quay with error: %+v", err)
				Expect(robotAccountExist).To(BeTrue(), "quay robot account does not exists")
			})
			It("created image repo is private", func() {
				isPublic, err := build.IsImageRepoPublic(imageRepoName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed while checking if the image repo %s is private", imageRepoName))
				Expect(isPublic).To(BeFalse(), "Expected image repo to be private, but it is public")
			})
			// skipped due to RHTAPBUGS-978
			It("a related PipelineRun should be deleted after deleting the component", Pending, func() {
				timeout = time.Second * 60
				interval = time.Second * 1
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(defaultBranchTestComponentName, testNamespace, true)).To(Succeed())
				// Test removal of PipelineRun
				Eventually(func() error {
					plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(defaultBranchTestComponentName, applicationName, testNamespace, "")
					if err == nil {
						return fmt.Errorf("pipelinerun %s/%s is not removed yet", plr.GetNamespace(), plr.GetName())
					}
					return err
				}, timeout, constants.PipelineRunPollingInterval).Should(MatchError(ContainSubstring("no pipelinerun found")), fmt.Sprintf("timed out when waiting for the PipelineRun to be removed for Component %s/%s", testNamespace, defaultBranchTestComponentName))
			})
			// skipped due to RHTAPBUGS-978
			It("PR branch should not exist in the repo", Pending, func() {
				timeout = time.Second * 60
				interval = time.Second * 1
				branchName := constants.PaCPullRequestBranchPrefix + defaultBranchTestComponentName
				Eventually(func() bool {
					exists, err := f.AsKubeAdmin.CommonController.Github.ExistsRef(helloWorldComponentGitSourceRepoName, constants.PaCPullRequestBranchPrefix+defaultBranchTestComponentName)
					Expect(err).ShouldNot(HaveOccurred())
					return exists
				}, timeout, interval).Should(BeFalse(), fmt.Sprintf("timed out when waiting for the branch %s to be deleted from %s repository", branchName, helloWorldComponentGitSourceRepoName))
			})
			// skipped due to RHTAPBUGS-978
			It("related image repo and the robot account should be deleted after deleting the component", Pending, func() {
				timeout = time.Second * 10
				interval = time.Second * 1
				// Check image repo should not be deleted
				Consistently(func() (bool, error) {
					return build.DoesImageRepoExistInQuay(imageRepoName)
				}, timeout, interval).Should(BeFalse(), fmt.Sprintf("image repo %s got deleted while it should not have", imageRepoName))
				// Check robot account should be deleted
				timeout = time.Second * 60
				Eventually(func() (bool, error) {
					return build.DoesRobotAccountExistInQuay(robotAccountName)
				}, timeout, interval).Should(BeFalse(), fmt.Sprintf("timed out when checking if robot account %s got deleted", robotAccountName))

			})
		})

		When("a new Component with specified custom branch is created", Label("build-custom-branch"), func() {
			var outputImage string
			BeforeAll(func() {

				// create the build secret in the user namespace
				secretName := "build-secret"
				token := os.Getenv("GITHUB_TOKEN")
				secretAnnotations := map[string]string{
					"appstudio.redhat.com/scm.repository": os.Getenv("MY_GITHUB_ORG") + "/*",
				}
				err = createBuildSecret(f, secretName, secretAnnotations, token)
				Expect(err).ShouldNot(HaveOccurred())

				componentObj := appservice.ComponentSpec{
					ComponentName: componentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           helloWorldComponentGitSourceURL,
								Revision:      componentBaseBranchName,
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}
				// Create a component with Git Source URL, a specified git branch and marking delete-repo=true
				component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
				Expect(err).ShouldNot(HaveOccurred())
			})
			It("triggers a PipelineRun", func() {
				timeout = time.Second * 600
				interval = time.Second * 1
				Eventually(func() error {
					plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !plr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", plr.GetNamespace(), plr.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
			})
			It("should lead to a PaC init PR creation", func() {
				timeout = time.Second * 300
				interval = time.Second * 1

				Eventually(func() bool {
					prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(helloWorldComponentGitSourceRepoName)
					Expect(err).ShouldNot(HaveOccurred())

					for _, pr := range prs {
						if pr.Head.GetRef() == pacBranchName {
							prNumber = pr.GetNumber()
							prHeadSha = pr.Head.GetSHA()
							return true
						}
					}
					return false
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for init PaC PR (branch name '%s') to be created in %s repository", pacBranchName, helloWorldComponentGitSourceRepoName))
			})
			It("the PipelineRun should eventually finish successfully", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "",
					f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, plr)).To(Succeed())
				// in case the first pipelineRun attempt has failed and was retried, we need to update the git branch head ref
				prHeadSha = plr.Labels["pipelinesascode.tekton.dev/sha"]
			})
			It("image repo and robot account created successfully", func() {

				component, err := f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("could not get component %s in the %s namespace", componentName, testNamespace))

				annotations := component.GetAnnotations()
				imageRepoName, err = build.GetQuayImageName(annotations)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed to read image repo name from %+v", annotations))

				imageExist, err := build.DoesImageRepoExistInQuay(imageRepoName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed while checking if image repo exists in quay with error: %+v", err))
				Expect(imageExist).To(BeTrue(), fmt.Sprintf("quay image for repo %s does not exists", imageRepoName))

				robotAccountName = build.GetRobotAccountName(imageRepoName)
				robotAccountExist, err := build.DoesRobotAccountExistInQuay(robotAccountName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed while checking if robot account exists in quay with error: %+v", err))
				Expect(robotAccountExist).To(BeTrue(), fmt.Sprintf("quay robot account %s does not exists", robotAccountName))

			})
			It("created image repo is public", func() {
				isPublic, err := build.IsImageRepoPublic(imageRepoName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed while checking if the image repo %s is public", imageRepoName))
				Expect(isPublic).To(BeTrue(), "Expected image repo to changed to public, but it is private")
			})
			It("image tag is updated successfully", func() {
				// check if the image tag exists in quay
				plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())

				for _, p := range plr.Spec.Params {
					if p.Name == "output-image" {
						outputImage = p.Value.StringVal
					}
				}
				Expect(outputImage).ToNot(BeEmpty(), "output image %s of the component could not be found", outputImage)
				isExists, err := build.DoesTagExistsInQuay(outputImage)
				Expect(err).ShouldNot(HaveOccurred(), "error while checking if the output image %s exists in quay", outputImage)
				Expect(isExists).To(BeTrue(), "image tag does not exists in quay")
			})

			It("should ensure pruning labels are set", func() {
				plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())

				image, err := build.ImageFromPipelineRun(plr)
				Expect(err).ShouldNot(HaveOccurred())

				labels := image.Config.Config.Labels
				Expect(labels).ToNot(BeEmpty())

				expiration, ok := labels["quay.expires-after"]
				Expect(ok).To(BeTrue())
				Expect(expiration).To(Equal(utils.GetEnv(constants.IMAGE_TAG_EXPIRATION_ENV, constants.DefaultImageTagExpiration)))
			})
			It("eventually leads to the PipelineRun status report at Checks tab", func() {
				expectedCheckRunName := fmt.Sprintf("%s-%s", componentName, "on-pull-request")
				Expect(f.AsKubeAdmin.CommonController.Github.GetCheckRunConclusion(expectedCheckRunName, helloWorldComponentGitSourceRepoName, prHeadSha, prNumber)).To(Equal(constants.CheckrunConclusionSuccess))
			})
		})

		When("the PaC init branch is updated", Label("build-custom-branch"), func() {
			var createdFileSHA string

			BeforeAll(func() {
				fileToCreatePath := fmt.Sprintf(".tekton/%s-readme.md", componentName)
				createdFile, err := f.AsKubeAdmin.CommonController.Github.CreateFile(helloWorldComponentGitSourceRepoName, fileToCreatePath, fmt.Sprintf("test PaC branch %s update", pacBranchName), pacBranchName)
				Expect(err).NotTo(HaveOccurred())

				createdFileSHA = createdFile.GetSHA()
				GinkgoWriter.Println("created file sha:", createdFileSHA)
			})

			It("eventually leads to triggering another PipelineRun", func() {
				timeout = time.Minute * 5

				Eventually(func() error {
					plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, createdFileSHA)
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !plr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", plr.GetNamespace(), plr.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
			})
			It("should lead to a PaC init PR update", func() {
				timeout = time.Second * 300
				interval = time.Second * 1

				Eventually(func() bool {
					prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(helloWorldComponentGitSourceRepoName)
					Expect(err).ShouldNot(HaveOccurred())

					for _, pr := range prs {
						if pr.Head.GetRef() == pacBranchName {
							Expect(prHeadSha).NotTo(Equal(pr.Head.GetSHA()))
							prNumber = pr.GetNumber()
							prHeadSha = pr.Head.GetSHA()
							return true
						}
					}
					return false
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for init PaC PR (branch name '%s') to be created in %s repository", pacBranchName, helloWorldComponentGitSourceRepoName))
			})
			It("PipelineRun should eventually finish", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, createdFileSHA,
					f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, plr)).To(Succeed())
				// in case the first pipelineRun attempt has failed and was retried, we need to update the git branch head ref
				createdFileSHA = plr.Labels["pipelinesascode.tekton.dev/sha"]
			})
			It("eventually leads to another update of a PR about the PipelineRun status report at Checks tab", func() {
				expectedCheckRunName := fmt.Sprintf("%s-%s", componentName, "on-pull-request")
				Expect(f.AsKubeAdmin.CommonController.Github.GetCheckRunConclusion(expectedCheckRunName, helloWorldComponentGitSourceRepoName, createdFileSHA, prNumber)).To(Equal(constants.CheckrunConclusionSuccess))
			})
		})

		When("the PaC init branch is merged", Label("build-custom-branch"), func() {
			var mergeResult *github.PullRequestMergeResult
			var mergeResultSha string

			BeforeAll(func() {
				Eventually(func() error {
					mergeResult, err = f.AsKubeAdmin.CommonController.Github.MergePullRequest(helloWorldComponentGitSourceRepoName, prNumber)
					return err
				}, time.Minute).Should(BeNil(), fmt.Sprintf("error when merging PaC pull request #%d in repo %s", prNumber, helloWorldComponentGitSourceRepoName))

				mergeResultSha = mergeResult.GetSHA()
				GinkgoWriter.Println("merged result sha:", mergeResultSha)
			})

			It("eventually leads to triggering another PipelineRun", func() {
				timeout = time.Minute * 10

				Eventually(func() error {
					plr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, mergeResultSha)
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !plr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", plr.GetNamespace(), plr.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
			})

			It("pipelineRun should eventually finish", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component,
					mergeResultSha, f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, plr)).To(Succeed())
				mergeResultSha = plr.Labels["pipelinesascode.tekton.dev/sha"]
			})

			It("does not have expiration set", func() {
				image, err := build.ImageFromPipelineRun(plr)
				Expect(err).ShouldNot(HaveOccurred())

				labels := image.Config.Config.Labels
				Expect(labels).ToNot(BeEmpty())

				expiration, ok := labels["quay.expires-after"]
				Expect(ok).To(BeFalse())
				Expect(expiration).To(BeEmpty())
			})

			It("After updating image visibility to private, it should not trigger another PipelineRun", func() {
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
				Eventually(func() error {
					err := f.AsKubeAdmin.HasController.SetComponentAnnotation(componentName, controllers.ImageRepoGenerateAnnotationName, constants.ImageControllerAnnotationRequestPrivateRepo[controllers.ImageRepoGenerateAnnotationName], testNamespace)
					if err != nil {
						GinkgoWriter.Printf("failed to update the component %s with error %v\n", componentName, err)
						return err
					}
					return nil
				}, time.Second*20, time.Second*1).Should(Succeed(), fmt.Sprintf("timed out when trying to update the component %s/%s", testNamespace, componentName))

				Consistently(func() bool {
					componentPipelineRun, _ := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
					return componentPipelineRun == nil
				}, time.Minute, constants.PipelineRunPollingInterval).Should(BeTrue(), fmt.Sprintf("expected no PipelineRun to be triggered for the component %s in %s namespace", componentName, testNamespace))
			})
			It("check image repo status after switching to private", func() {
				var imageStatus imagecontollers.ImageRepositoryStatus
				Eventually(func() (bool, error) {
					component, err := f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
					if err != nil {
						GinkgoWriter.Printf("error while getting component: %v\n", err)
						return false, err
					}

					imageAnnotationValue := component.Annotations[controllers.ImageRepoAnnotationName]
					GinkgoWriter.Printf("image annotation value: %s\n", imageAnnotationValue)
					statusBytes := []byte(imageAnnotationValue)

					err = json.Unmarshal(statusBytes, &imageStatus)
					if err != nil {
						GinkgoWriter.Printf("cannot unmarshal image status: %v\n", err)
						return false, err
					}
					if imageStatus.Message != "" && strings.Contains(imageStatus.Message, "Quay organization plan doesn't allow private image repositories") {
						return false, fmt.Errorf("failed to switch to private image")
					}
					return true, nil
				}, time.Second*20, time.Second*2).Should(BeTrue(), "component image status annotation has unexpected content")
			})
			It("image repo is updated to private", func() {
				isPublic, err := build.IsImageRepoPublic(imageRepoName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed while checking if the image repo %s is private", imageRepoName))
				Expect(isPublic).To(BeFalse(), "Expected image repo to changed to private, but it is public")
			})

		})

		When("the component is removed and recreated (with the same name in the same namespace)", Label("build-custom-branch"), func() {
			BeforeAll(func() {
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, true)).To(Succeed())

				timeout = 1 * time.Minute
				interval = 1 * time.Second
				Eventually(func() bool {
					_, err := f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
					return k8sErrors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for the app %s/%s to be deleted", testNamespace, applicationName))
				// Check removal of image repo
				Eventually(func() (bool, error) {
					return build.DoesImageRepoExistInQuay(imageRepoName)
				}, timeout, interval).Should(BeFalse(), fmt.Sprintf("timed out when waiting for image repo %s to be deleted", imageRepoName))
				// Check removal of robot account
				Eventually(func() (bool, error) {
					return build.DoesRobotAccountExistInQuay(robotAccountName)
				}, timeout, interval).Should(BeFalse(), fmt.Sprintf("timed out when waiting for robot account %s to be deleted", robotAccountName))

				componentObj := appservice.ComponentSpec{
					ComponentName: componentName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           helloWorldComponentGitSourceURL,
								Revision:      componentBaseBranchName,
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}
				_, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("should no longer lead to a creation of a PaC PR", func() {
				timeout = time.Second * 10
				interval = time.Second * 2
				Consistently(func() error {
					prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(helloWorldComponentGitSourceRepoName)
					Expect(err).ShouldNot(HaveOccurred())

					for _, pr := range prs {
						if pr.Head.GetRef() == pacBranchName {
							return fmt.Errorf("did not expect a new PR created in %s repository after initial PaC configuration was already merged for the same component name and a namespace", helloWorldComponentGitSourceRepoName)
						}
					}
					return nil
				}, timeout, interval).Should(BeNil())
			})
		})
	})

	Describe("test pac with multiple components using same repository", Ordered, Label("pac-build", "multi-component"), func() {
		var applicationName, testNamespace, multiComponentBaseBranchName, multiComponentPRBranchName, mergeResultSha string
		var pacBranchNames []string
		var prNumber int
		var mergeResult *github.PullRequestMergeResult
		var timeout time.Duration

		BeforeAll(func() {
			if os.Getenv(constants.SKIP_PAC_TESTS_ENV) == "true" {
				Skip("Skipping this test due to configuration issue with Spray proxy")
			}
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			if utils.IsPrivateHostname(f.OpenshiftConsoleHost) {
				Skip("Using private cluster (not reachable from Github), skipping...")
			}

			applicationName = fmt.Sprintf("build-suite-positive-mc-%s", util.GenerateRandomString(4))
			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			multiComponentBaseBranchName = fmt.Sprintf("multi-component-base-%s", util.GenerateRandomString(6))
			err = f.AsKubeAdmin.CommonController.Github.CreateRef(multiComponentGitSourceRepoName, multiComponentDefaultBranch, multiComponentGitRevision, multiComponentBaseBranchName)
			Expect(err).ShouldNot(HaveOccurred())

			//Branch for creating pull request
			multiComponentPRBranchName = fmt.Sprintf("%s-%s", "pr-branch", util.GenerateRandomString(6))

		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}

			// Delete new branches created by PaC and a testing branch used as a component's base branch
			for _, pacBranchName := range pacBranchNames {
				err = f.AsKubeAdmin.CommonController.Github.DeleteRef(multiComponentGitSourceRepoName, pacBranchName)
				if err != nil {
					Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
				}
			}
			// Delete the created base branch
			err = f.AsKubeAdmin.CommonController.Github.DeleteRef(multiComponentGitSourceRepoName, multiComponentBaseBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}
			// Delete the created pr branch
			err = f.AsKubeAdmin.CommonController.Github.DeleteRef(multiComponentGitSourceRepoName, multiComponentPRBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}
		})

		When("components are created in same namespace", func() {
			var component *appservice.Component

			for _, contextDir := range multiComponentContextDirs {
				contextDir := contextDir
				componentName := fmt.Sprintf("%s-%s", contextDir, util.GenerateRandomString(6))
				pacBranchName := constants.PaCPullRequestBranchPrefix + componentName
				pacBranchNames = append(pacBranchNames, pacBranchName)

				It(fmt.Sprintf("creates component with context directory %s", contextDir), func() {
					componentObj := appservice.ComponentSpec{
						ComponentName: componentName,
						Application:   applicationName,
						Source: appservice.ComponentSource{
							ComponentSourceUnion: appservice.ComponentSourceUnion{
								GitSource: &appservice.GitSource{
									URL:           multiComponentGitSourceURL,
									Revision:      multiComponentBaseBranchName,
									Context:       contextDir,
									DockerfileURL: constants.DockerFilePath,
								},
							},
						},
					}
					component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
					Expect(err).ShouldNot(HaveOccurred())
				})

				It(fmt.Sprintf("triggers a PipelineRun for component %s", componentName), func() {
					timeout = time.Minute * 5
					Eventually(func() error {
						pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
						if err != nil {
							GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
							return err
						}
						if !pr.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
						}
						return nil
					}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", componentName, testNamespace))
				})

				It(fmt.Sprintf("should lead to a PaC PR creation for component %s", componentName), func() {
					timeout = time.Second * 300
					interval := time.Second * 1

					Eventually(func() bool {
						prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(multiComponentGitSourceRepoName)
						Expect(err).ShouldNot(HaveOccurred())

						for _, pr := range prs {
							if pr.Head.GetRef() == pacBranchName {
								prNumber = pr.GetNumber()
								return true
							}
						}
						return false
					}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for PaC PR (branch name '%s') to be created in %s repository", pacBranchName, multiComponentGitSourceRepoName))
				})

				It(fmt.Sprintf("the PipelineRun should eventually finish successfully for component %s", componentName), func() {
					Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "",
						f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
				})

				It("merging the PR should be successful", func() {
					Eventually(func() error {
						mergeResult, err = f.AsKubeAdmin.CommonController.Github.MergePullRequest(multiComponentGitSourceRepoName, prNumber)
						return err
					}, time.Minute).Should(BeNil(), fmt.Sprintf("error when merging PaC pull request #%d in repo %s", prNumber, multiComponentGitSourceRepoName))

					mergeResultSha = mergeResult.GetSHA()
					GinkgoWriter.Printf("merged result sha: %s for PR #%d\n", mergeResultSha, prNumber)

				})
				It("leads to triggering on push PipelineRun", func() {
					timeout = time.Minute * 5

					Eventually(func() error {
						pipelineRun, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, mergeResultSha)
						if err != nil {
							GinkgoWriter.Printf("Push PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
							return err
						}
						if !pipelineRun.HasStarted() {
							return fmt.Errorf("push pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
						}
						return nil
					}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
				})
			}
			It("only one component is changed", func() {
				//Delete all the pipelineruns in the namespace before sending PR
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
				//Create the ref, add the file and create the PR
				err = f.AsKubeAdmin.CommonController.Github.CreateRef(multiComponentGitSourceRepoName, multiComponentDefaultBranch, mergeResultSha, multiComponentPRBranchName)
				Expect(err).ShouldNot(HaveOccurred())
				fileToCreatePath := fmt.Sprintf("%s/sample-file.txt", multiComponentContextDirs[0])
				createdFileSha, err := f.AsKubeAdmin.CommonController.Github.CreateFile(multiComponentGitSourceRepoName, fileToCreatePath, fmt.Sprintf("sample test file inside %s", multiComponentContextDirs[0]), multiComponentPRBranchName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("error while creating file: %s", fileToCreatePath))
				pr, err := f.AsKubeAdmin.CommonController.Github.CreatePullRequest(multiComponentGitSourceRepoName, "sample pr title", "sample pr body", multiComponentPRBranchName, multiComponentBaseBranchName)
				Expect(err).ShouldNot(HaveOccurred())
				GinkgoWriter.Printf("PR #%d got created with sha %s\n", pr.GetNumber(), createdFileSha.GetSHA())
			})
			It("only related pipelinerun should be triggered", func() {
				Eventually(func() error {
					pipelineRuns, err := f.AsKubeAdmin.HasController.GetAllPipelineRunsForApplication(applicationName, testNamespace)
					if err != nil {
						GinkgoWriter.Println("on pull PiplelineRun has not been created yet for the PR")
						return err
					}
					if len(pipelineRuns.Items) != 1 || !strings.HasPrefix(pipelineRuns.Items[0].Name, multiComponentContextDirs[0]) {
						return fmt.Errorf("pipelinerun created in the namespace %s is not as expected, got pipelineruns %v", testNamespace, pipelineRuns.Items)
					}
					return nil
				}, time.Minute*5, constants.PipelineRunPollingInterval).Should(Succeed(), "timeout while waiting for PR pipeline to start")
			})
		})
		When("a components is created with same git url in different namespace", func() {
			var namespace, appName, compName string
			var fw *framework.Framework

			BeforeAll(func() {
				fw, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
				Expect(err).NotTo(HaveOccurred())
				namespace = fw.UserNamespace

				appName = fmt.Sprintf("build-suite-negative-mc-%s", util.GenerateRandomString(4))
				_, err = f.AsKubeAdmin.HasController.CreateApplication(appName, namespace)
				Expect(err).NotTo(HaveOccurred())

				compName = fmt.Sprintf("%s-%s", multiComponentContextDirs[0], util.GenerateRandomString(6))

				componentObj := appservice.ComponentSpec{
					ComponentName: compName,
					Application:   appName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           multiComponentGitSourceURL,
								Revision:      multiComponentBaseBranchName,
								Context:       multiComponentContextDirs[0],
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}
				_, err = fw.AsKubeAdmin.HasController.CreateComponent(componentObj, namespace, "", "", appName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
				Expect(err).ShouldNot(HaveOccurred())

			})

			AfterAll(func() {
				if !CurrentSpecReport().Failed() {
					Expect(fw.AsKubeAdmin.HasController.DeleteApplication(appName, namespace, false)).To(Succeed())
					Expect(fw.SandboxController.DeleteUserSignup(fw.UserName)).To(BeTrue())
				}
			})

			It("should fail to configure PaC for the component", func() {
				var buildStatus *controllers.BuildStatus

				Eventually(func() (bool, error) {
					component, err := fw.AsKubeAdmin.HasController.GetComponent(compName, namespace)
					if err != nil {
						GinkgoWriter.Printf("error while getting the component: %v\n", err)
						return false, err
					}

					buildStatusAnnotationValue := component.Annotations[controllers.BuildStatusAnnotationName]
					GinkgoWriter.Printf(buildStatusAnnotationValueLoggingFormat, buildStatusAnnotationValue)
					statusBytes := []byte(buildStatusAnnotationValue)

					err = json.Unmarshal(statusBytes, &buildStatus)
					if err != nil {
						GinkgoWriter.Printf("cannot unmarshal build status from component annotation: %v\n", err)
						return false, err
					}

					GinkgoWriter.Printf("build status: %+v\n", buildStatus.PaC)

					return buildStatus.PaC != nil && buildStatus.PaC.State == "error" && strings.Contains(buildStatus.PaC.ErrMessage, "Git repository is already handled by Pipelines as Code"), nil
				}, time.Minute*2, time.Second*2).Should(BeTrue(), "build status is unexpected")

			})

		})

	})
	Describe("test build secret lookup", Label("pac-build", "secret-lookup"), Ordered, func() {
		var testNamespace, applicationName, firstComponentBaseBranchName, secondComponentBaseBranchName, firstComponentName, secondComponentName, firstPacBranchName, secondPacBranchName string
		BeforeAll(func() {
			if os.Getenv(constants.SKIP_PAC_TESTS_ENV) == "true" {
				Skip("Skipping this test due to configuration issue with Spray proxy")
			}
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			applicationName = fmt.Sprintf("build-secret-lookup-%s", util.GenerateRandomString(4))
			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			firstComponentBaseBranchName = fmt.Sprintf("component-one-base-%s", util.GenerateRandomString(6))
			err = f.AsKubeAdmin.CommonController.Github.CreateRefInOrg(noAppOrgName, secretLookupGitSourceRepoOneName, secretLookupDefaultBranchOne, secretLookupGitRevisionOne, firstComponentBaseBranchName)
			Expect(err).ShouldNot(HaveOccurred())

			secondComponentBaseBranchName = fmt.Sprintf("component-two-base-%s", util.GenerateRandomString(6))
			err = f.AsKubeAdmin.CommonController.Github.CreateRefInOrg(noAppOrgName, secretLookupGitSourceRepoTwoName, secretLookupDefaultBranchTwo, secretLookupGitRevisionTwo, secondComponentBaseBranchName)
			Expect(err).ShouldNot(HaveOccurred())

		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}

			// Delete new branches created by PaC
			err = f.AsKubeAdmin.CommonController.Github.DeleteRefFromOrg(noAppOrgName, secretLookupGitSourceRepoOneName, firstPacBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}
			err = f.AsKubeAdmin.CommonController.Github.DeleteRefFromOrg(noAppOrgName, secretLookupGitSourceRepoTwoName, secondPacBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}

			// Delete the created first component base branch
			err = f.AsKubeAdmin.CommonController.Github.DeleteRefFromOrg(noAppOrgName, secretLookupGitSourceRepoOneName, firstComponentBaseBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}
			// Delete the created second component base branch
			err = f.AsKubeAdmin.CommonController.Github.DeleteRefFromOrg(noAppOrgName, secretLookupGitSourceRepoTwoName, secondComponentBaseBranchName)
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
			}

			// Delete created webhook from GitHub
			cleanupWebhooks(f, secretLookupGitSourceRepoTwoName)

		})
		When("two secrets are created", func() {
			BeforeAll(func() {
				// create the correct build secret for second component
				secretName1 := "build-secret-1"
				secretAnnotations := map[string]string{
					"appstudio.redhat.com/scm.repository": noAppOrgName + "/" + secretLookupGitSourceRepoTwoName,
				}
				token := os.Getenv("GITHUB_TOKEN")
				err = createBuildSecret(f, secretName1, secretAnnotations, token)
				Expect(err).ShouldNot(HaveOccurred())

				// create incorrect build-secret for the first component
				secretName2 := "build-secret-2"
				dummyToken := "ghp_dummy_secret"
				err = createBuildSecret(f, secretName2, nil, dummyToken)
				Expect(err).ShouldNot(HaveOccurred())

				// component names and pac branch names
				firstComponentName = fmt.Sprintf("%s-%s", "component-one", util.GenerateRandomString(4))
				secondComponentName = fmt.Sprintf("%s-%s", "component-two", util.GenerateRandomString(4))
				firstPacBranchName = constants.PaCPullRequestBranchPrefix + firstComponentName
				secondPacBranchName = constants.PaCPullRequestBranchPrefix + secondComponentName
			})

			It("creates first component", func() {
				componentObj1 := appservice.ComponentSpec{
					ComponentName: firstComponentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           secretLookupComponentOneGitSourceURL,
								Revision:      firstComponentBaseBranchName,
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}
				_, err := f.AsKubeAdmin.HasController.CreateComponent(componentObj1, testNamespace, "", "", applicationName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
				Expect(err).ShouldNot(HaveOccurred())
			})
			It("creates second component", func() {
				componentObj2 := appservice.ComponentSpec{
					ComponentName: secondComponentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           secretLookupComponentTwoGitSourceURL,
								Revision:      secondComponentBaseBranchName,
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}
				_, err := f.AsKubeAdmin.HasController.CreateComponent(componentObj2, testNamespace, "", "", applicationName, false, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("check first component annotation has errors", func() {
				buildStatus := &controllers.BuildStatus{}
				Eventually(func() (bool, error) {
					component, err := f.AsKubeAdmin.HasController.GetComponent(firstComponentName, testNamespace)
					if err != nil {
						return false, err
					} else if component == nil {
						return false, fmt.Errorf("got component as nil after getting component %s in namespace %s", firstComponentName, testNamespace)
					}
					buildStatusAnnotationValue := component.Annotations[controllers.BuildStatusAnnotationName]
					GinkgoWriter.Printf(buildStatusAnnotationValueLoggingFormat, buildStatusAnnotationValue)
					statusBytes := []byte(buildStatusAnnotationValue)
					err = json.Unmarshal(statusBytes, buildStatus)
					if err != nil {
						return false, err
					}
					return buildStatus.PaC != nil && buildStatus.PaC.State == "error" && strings.Contains(buildStatus.PaC.ErrMessage, "Access token is unrecognizable by GitHub"), nil
				}, time.Minute*2, 5*time.Second).Should(BeTrue(), "failed while checking build status for component %q is correct", firstComponentName)
			})

			It(fmt.Sprintf("triggered PipelineRun is for component %s", secondComponentName), func() {
				timeout = time.Minute * 5
				Eventually(func() error {
					pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(secondComponentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, secondComponentName)
						return err
					}
					if !pr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", secondComponentName, testNamespace))
			})

			It("check only one pipelinerun should be triggered", func() {
				// Waiting for 2 minute to see if only one pipelinerun is triggered
				Consistently(func() (bool, error) {
					pipelineRuns, err := f.AsKubeAdmin.HasController.GetAllPipelineRunsForApplication(applicationName, testNamespace)
					if err != nil {
						return false, err
					}
					if len(pipelineRuns.Items) != 1 {
						return false, fmt.Errorf("plr count in the namespace %s is not one, got pipelineruns %v", testNamespace, pipelineRuns.Items)
					}
					return true, nil
				}, time.Minute*2, constants.PipelineRunPollingInterval).Should(BeTrue(), "timeout while checking if any more pipelinerun is triggered")
			})
		})
	})
	Describe("test build annotations", Label("annotations"), Ordered, func() {
		var testNamespace, componentName, applicationName string
		var componentObj appservice.ComponentSpec
		var component *appservice.Component

		var timeout, interval time.Duration

		BeforeAll(func() {
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).ShouldNot(HaveOccurred())
			testNamespace = f.UserNamespace

			timeout = 5 * time.Minute
			interval = 5 * time.Second

			applicationName = fmt.Sprintf("build-suite-test-application-%s", util.GenerateRandomString(4))
			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			componentName = fmt.Sprintf("%s-%s", "test-annotations", util.GenerateRandomString(6))

		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}

		})

		When("component is created", func() {
			var lastBuildStartTime string

			BeforeAll(func() {
				componentObj = appservice.ComponentSpec{
					ComponentName: componentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           annotationsTestGitSourceURL,
								Revision:      annotationsTestRevision,
								DockerfileURL: constants.DockerFilePath,
							},
						},
					},
				}

				component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, constants.DefaultDockerBuildPipelineBundle)
				Expect(component).ToNot(BeNil())
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("triggers a pipeline run", func() {
				Eventually(func() error {
					pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !pr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
					}
					return nil
				}, time.Minute*5, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
			})

			It("component build status annotation is set correctly", func() {
				var buildStatus *controllers.BuildStatus

				Eventually(func() (bool, error) {
					component, err := f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
					if err != nil {
						GinkgoWriter.Printf("cannot get the component: %v\n", err)
						return false, err
					}

					buildStatusAnnotationValue := component.Annotations[controllers.BuildStatusAnnotationName]
					GinkgoWriter.Printf(buildStatusAnnotationValueLoggingFormat, buildStatusAnnotationValue)
					statusBytes := []byte(buildStatusAnnotationValue)

					err = json.Unmarshal(statusBytes, &buildStatus)
					if err != nil {
						GinkgoWriter.Printf("cannot unmarshal build status: %v\n", err)
						return false, err
					}

					if buildStatus.Simple != nil {
						GinkgoWriter.Printf("buildStartTime: %s\n", buildStatus.Simple.BuildStartTime)
						lastBuildStartTime = buildStatus.Simple.BuildStartTime
					} else {
						GinkgoWriter.Println("build status does not have simple field")
					}

					return buildStatus.Simple != nil && buildStatus.Simple.BuildStartTime != "", nil
				}, timeout, interval).Should(BeTrue(), "build status has unexpected content")

				//Expect pipelinerun count to be 1
				Eventually(func() error {
					pipelineRuns, err := f.AsKubeAdmin.HasController.GetAllPipelineRunsForApplication(applicationName, testNamespace)
					if err != nil {
						GinkgoWriter.Println("PiplelineRun has not been created yet")
						return err
					}
					if len(pipelineRuns.Items) != 1 {
						return fmt.Errorf("pipelinerun count in the namespace %s is not one, got pipelineruns %v", testNamespace, pipelineRuns.Items)
					}
					return nil
				}, time.Minute*5, constants.PipelineRunPollingInterval).Should(Succeed(), "timeout while waiting for first pipelinerun to start")
			})

			Specify("simple build can be triggered manually", func() {
				// Wait 1 second before sending the second build request, so that we get different buildStatus.Simple.BuildStartTime timestamp
				time.Sleep(1 * time.Second)
				Expect(f.AsKubeAdmin.HasController.SetComponentAnnotation(componentName, controllers.BuildRequestAnnotationName, controllers.BuildRequestTriggerSimpleBuildAnnotationValue, testNamespace)).To(Succeed())
			})

			It("another pipelineRun is triggered", func() {
				//Expect pipelinerun count to be 2
				Eventually(func() error {
					pipelineRuns, err := f.AsKubeAdmin.HasController.GetAllPipelineRunsForApplication(applicationName, testNamespace)
					if err != nil {
						GinkgoWriter.Println("Second piplelineRun has not been created yet")
						return err
					}
					if len(pipelineRuns.Items) != 2 {
						return fmt.Errorf("pipelinerun count in the namespace %s is not two, got pipelineruns %v", testNamespace, pipelineRuns.Items)
					}
					return nil
				}, time.Minute*5, constants.PipelineRunPollingInterval).Should(Succeed(), "timeout while waiting for second pipelinerun to start")
			})

			It("component build annotation is correct", func() {
				var buildStatus *controllers.BuildStatus

				Eventually(func() (bool, error) {
					component, err := f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
					if err != nil {
						GinkgoWriter.Printf("cannot get the component: %v\n", err)
						return false, err
					}

					buildStatusAnnotationValue := component.Annotations[controllers.BuildStatusAnnotationName]
					GinkgoWriter.Printf(buildStatusAnnotationValueLoggingFormat, buildStatusAnnotationValue)
					statusBytes := []byte(buildStatusAnnotationValue)

					err = json.Unmarshal(statusBytes, &buildStatus)
					if err != nil {
						GinkgoWriter.Printf("cannot unmarshal build status: %v\n", err)
						return false, err
					}

					if buildStatus.Simple != nil {
						GinkgoWriter.Printf("buildStartTime: %s\n", buildStatus.Simple.BuildStartTime)
					} else {
						GinkgoWriter.Println("build status does not have simple field")
					}

					return buildStatus.Simple != nil && buildStatus.Simple.BuildStartTime != lastBuildStartTime, nil
				}, timeout, interval).Should(BeTrue(), "build status has unexpected content")
			})

			It("handles invalid request annotation", func() {

				invalidAnnotation := "foo"
				expectedInvalidAnnotationMessage := fmt.Sprintf("unexpected build request: %s", invalidAnnotation)

				Expect(f.AsKubeAdmin.HasController.SetComponentAnnotation(componentName, controllers.BuildRequestAnnotationName, invalidAnnotation, testNamespace)).To(Succeed())

				// Waiting for 2 minute to see if any more pipelinerun is triggered
				Consistently(func() (bool, error) {
					pipelineRuns, err := f.AsKubeAdmin.HasController.GetAllPipelineRunsForApplication(applicationName, testNamespace)
					if err != nil {
						return false, err
					}
					if len(pipelineRuns.Items) != 2 {
						return false, fmt.Errorf("pipelinerun count in the namespace %s is not two, got pipelineruns %v", testNamespace, pipelineRuns.Items)
					}
					return true, nil
				}, time.Minute*2, constants.PipelineRunPollingInterval).Should(BeTrue(), "timeout while checking if any more pipelinerun is triggered")

				buildStatus := &controllers.BuildStatus{}
				Eventually(func() error {
					component, err = f.AsKubeAdmin.HasController.GetComponent(componentName, testNamespace)
					if err != nil {
						return err
					} else if component == nil {
						return fmt.Errorf("got component as nil after getting component %s in namespace %s", componentName, testNamespace)
					}
					buildStatusAnnotationValue := component.Annotations[controllers.BuildStatusAnnotationName]
					GinkgoWriter.Printf(buildStatusAnnotationValueLoggingFormat, buildStatusAnnotationValue)
					statusBytes := []byte(buildStatusAnnotationValue)
					err = json.Unmarshal(statusBytes, buildStatus)
					if err != nil {
						return err
					}
					if !strings.Contains(buildStatus.Message, expectedInvalidAnnotationMessage) {
						return fmt.Errorf("build status message is not as expected, got: %q, expected: %q", buildStatus.Message, expectedInvalidAnnotationMessage)
					}
					return nil
				}, time.Minute*2, 2*time.Second).Should(Succeed(), "failed while checking build status message for component %q is correct after setting invalid annotations", componentName)
			})
		})
	})

	Describe("Creating component with container image source", Ordered, func() {
		var applicationName, componentName, testNamespace string
		var timeout time.Duration

		BeforeAll(func() {
			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			componentName = fmt.Sprintf("build-suite-test-component-image-source-%s", util.GenerateRandomString(6))
			outputContainerImage := ""
			timeout = time.Second * 10
			// Create a component with containerImageSource being defined
			component := appservice.ComponentSpec{
				ComponentName:  fmt.Sprintf("build-suite-test-component-image-source-%s", util.GenerateRandomString(6)),
				ContainerImage: containerImageSource,
			}
			_, err = f.AsKubeAdmin.HasController.CreateComponent(component, testNamespace, outputContainerImage, "", applicationName, true, constants.DefaultDockerBuildPipelineBundle)
			Expect(err).ShouldNot(HaveOccurred())
		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}
		})

		It("should not trigger a PipelineRun", func() {
			Consistently(func() bool {
				_, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).To(HaveOccurred())
				return strings.Contains(err.Error(), "no pipelinerun found")
			}, timeout, constants.PipelineRunPollingInterval).Should(BeTrue(), fmt.Sprintf("expected no PipelineRun to be triggered for the component %s in %s namespace", componentName, testNamespace))
		})
	})

	Describe("A secret with dummy quay.io credentials is created in the testing namespace", Ordered, func() {

		var applicationName, componentName, testNamespace string
		var timeout time.Duration
		var err error
		var pr *pipeline.PipelineRun

		BeforeAll(func() {

			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			if err = f.AsKubeAdmin.CommonController.UnlinkSecretFromServiceAccount(testNamespace, constants.RegistryAuthSecretName, constants.DefaultPipelineServiceAccount, true); err != nil {
				GinkgoWriter.Println(fmt.Sprintf("Failed to unlink registry auth secret from service account: %v\n", err))
			}

			if err = f.AsKubeAdmin.CommonController.DeleteSecret(testNamespace, constants.RegistryAuthSecretName); err != nil {
				GinkgoWriter.Println(fmt.Sprintf("Failed to delete regitry auth secret from namespace: %s\n", err))
			}

			_, err := f.AsKubeAdmin.CommonController.GetSecret(testNamespace, constants.RegistryAuthSecretName)
			if err != nil {
				// If we have an error when getting RegistryAuthSecretName, it should be IsNotFound err
				Expect(k8sErrors.IsNotFound(err)).To(BeTrue())
			} else {
				Skip("a registry auth secret is already created in testing namespace - skipping....")
			}

			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))

			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			timeout = time.Minute * 5

			dummySecret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: constants.RegistryAuthSecretName},
				Type:       v1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{".dockerconfigjson": []byte("{\"auths\":{\"quay.io\":{\"username\":\"test\",\"password\":\"test\",\"auth\":\"dGVzdDp0ZXN0\",\"email\":\"\"}}}")},
			}

			_, err = f.AsKubeAdmin.CommonController.CreateSecret(testNamespace, dummySecret)
			Expect(err).ToNot(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(testNamespace, dummySecret.Name, constants.DefaultPipelineServiceAccount, false)
			Expect(err).ToNot(HaveOccurred())

			componentName = "build-suite-test-secret-overriding"
			componentObj := appservice.ComponentSpec{
				ComponentName: componentName,
				Application:   applicationName,
				Source: appservice.ComponentSource{
					ComponentSourceUnion: appservice.ComponentSourceUnion{
						GitSource: &appservice.GitSource{
							URL:           helloWorldComponentGitSourceURL,
							DockerfileURL: constants.DockerFilePath,
						},
					},
				},
			}
			_, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, true, constants.DefaultDockerBuildPipelineBundle)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}
		})

		It("should override the shared secret", func() {
			Eventually(func() error {
				pr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				if err != nil {
					GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
					return err
				}
				if !pr.HasStarted() {
					return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
				}
				return nil
			}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))

			pr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(pr.Spec.Workspaces).To(HaveLen(1))
		})

		It("should not be possible to push to quay.io repo (PipelineRun should fail)", func() {
			pipelineRunTimeout := int(time.Duration(20) * time.Minute)

			Expect(f.AsKubeAdmin.TektonController.WatchPipelineRun(pr.Name, testNamespace, pipelineRunTimeout)).To(Succeed())
			pr, err = f.AsKubeAdmin.TektonController.GetPipelineRun(pr.GetName(), pr.GetNamespace())
			Expect(err).NotTo(HaveOccurred())
			tr, err := f.AsKubeAdmin.TektonController.GetTaskRunStatus(f.AsKubeAdmin.CommonController.KubeRest(), pr, constants.BuildTaskRunName)
			Expect(err).NotTo(HaveOccurred())
			Expect(tekton.DidTaskRunSucceed(tr)).To(BeFalse())
		})
	})

	Describe("test of component update with renovate", Ordered, Label("renovate", "multi-component"), func() {
		type multiComponent struct {
			repoName        string
			baseBranch      string
			componentBranch string
			baseRevision    string
			componentName   string
			gitRepo         string
			pacBranchName   string
			component       *appservice.Component
		}

		ChildComponentDef := multiComponent{repoName: componentDependenciesChildRepoName, baseRevision: componentDependenciesChildGitRevision, baseBranch: componentDependenciesChildDefaultBranch}
		ParentComponentDef := multiComponent{repoName: componentDependenciesParentRepoName, baseRevision: componentDependenciesParentGitRevision, baseBranch: componentDependenciesParentDefaultBranch}
		components := []*multiComponent{&ChildComponentDef, &ParentComponentDef}
		var applicationName, testNamespace, mergeResultSha string
		var prNumber int
		var mergeResult *github.PullRequestMergeResult
		var timeout time.Duration
		var parentFirstDigest string
		var parentPostPacMergeDigest string
		var parentImageNameWithNoDigest string
		const distributionRepository = "quay.io/redhat-appstudio-qe/release-repository"
		quayOrg := utils.GetEnv("DEFAULT_QUAY_ORG", "")

		var managedNamespace string
		BeforeAll(func() {
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace

			applicationName = fmt.Sprintf("build-suite-component-update-%s", util.GenerateRandomString(4))
			_, err = f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			branchString := util.GenerateRandomString(4)
			ParentComponentDef.componentBranch = fmt.Sprintf("multi-component-parent-base-%s", branchString)
			ChildComponentDef.componentBranch = fmt.Sprintf("multi-component-child-base-%s", branchString)
			ParentComponentDef.gitRepo = fmt.Sprintf(githubUrlFormat, gihubOrg, ParentComponentDef.repoName)
			ChildComponentDef.gitRepo = fmt.Sprintf(githubUrlFormat, gihubOrg, ChildComponentDef.repoName)
			ParentComponentDef.componentName = fmt.Sprintf("multi-component-parent-%s", branchString)
			ChildComponentDef.componentName = fmt.Sprintf("multi-component-child-%s", branchString)
			ParentComponentDef.pacBranchName = constants.PaCPullRequestBranchPrefix + ParentComponentDef.componentName
			ChildComponentDef.pacBranchName = constants.PaCPullRequestBranchPrefix + ChildComponentDef.componentName

			for _, i := range components {
				println("creating branch " + i.componentBranch)
				err = f.AsKubeAdmin.CommonController.Github.CreateRef(i.repoName, i.baseBranch, i.baseRevision, i.componentBranch)
				Expect(err).ShouldNot(HaveOccurred())
			}
			// Also setup a release namespace so we can test nudging of distribution repository images
			managedNamespace = testNamespace + "-managed"
			_, err = f.AsKubeAdmin.CommonController.CreateTestNamespace(managedNamespace)
			Expect(err).ShouldNot(HaveOccurred())

			// We just need the ReleaseAdmissionPlan to contain a mapping between component and distribution repositories
			data := struct {
				Mapping struct {
					Components []struct {
						Name       string
						Repository string
					}
				}
			}{}
			data.Mapping.Components = append(data.Mapping.Components, struct {
				Name       string
				Repository string
			}{Name: ParentComponentDef.componentName, Repository: distributionRepository})
			rawData, err := json.Marshal(&data)

			GinkgoWriter.Printf("ReleaseAdmissionPlan data: %s", string(rawData))
			Expect(err).NotTo(HaveOccurred())
			_, err = f.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission("demo", managedNamespace, "", f.UserNamespace, "demo", constants.DefaultPipelineServiceAccount, []string{applicationName}, false, &tektonutils.PipelineRef{
				Resolver: "git",
				Params: []tektonutils.Param{
					{Name: "url", Value: releasecommon.RelSvcCatalogURL},
					{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
					{Name: "pathInRepo", Value: "pipelines/e2e/e2e.yaml"},
				}}, &runtime.RawExtension{Raw: rawData})
			Expect(err).NotTo(HaveOccurred())

		})

		AfterAll(func() {
			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}
			Expect(f.AsKubeAdmin.CommonController.DeleteNamespace(managedNamespace)).ShouldNot(HaveOccurred())

			// Delete new branches created by renovate and a testing branch used as a component's base branch
			for _, c := range components {
				println("deleting branch " + c.componentBranch)
				err = f.AsKubeAdmin.CommonController.Github.DeleteRef(c.repoName, c.componentBranch)
				if err != nil {
					Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
				}
				err = f.AsKubeAdmin.CommonController.Github.DeleteRef(c.repoName, c.pacBranchName)
				if err != nil {
					Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
				}
			}
		})

		When("components are created in same namespace", func() {

			It("creates component with nudges", func() {
				for _, comp := range components {
					componentObj := appservice.ComponentSpec{
						ComponentName: comp.componentName,
						Application:   applicationName,
						Source: appservice.ComponentSource{
							ComponentSourceUnion: appservice.ComponentSourceUnion{
								GitSource: &appservice.GitSource{
									URL:           comp.gitRepo,
									Revision:      comp.componentBranch,
									DockerfileURL: "Dockerfile",
								},
							},
						},
					}
					//make the parent repo nudge the child repo
					if comp.repoName == componentDependenciesParentRepoName {
						componentObj.BuildNudgesRef = []string{ChildComponentDef.componentName}
						comp.component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, true, utils.MergeMaps(utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo), constants.DefaultDockerBuildPipelineBundle))
					} else {
						comp.component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, true, utils.MergeMaps(constants.ImageControllerAnnotationRequestPublicRepo, constants.DefaultDockerBuildPipelineBundle))
					}
					Expect(err).ShouldNot(HaveOccurred())
				}
			})
			// Initial pipeline run, we need this so we have an initial image that we can then update
			It(fmt.Sprintf("triggers a PipelineRun for parent component %s", ParentComponentDef.componentName), func() {
				timeout = time.Minute * 5

				Eventually(func() error {
					pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(ParentComponentDef.componentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, ParentComponentDef.componentName)
						return err
					}
					if !pr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", ParentComponentDef.componentName, testNamespace))
			})
			It(fmt.Sprintf("the PipelineRun should eventually finish successfully for parent component %s", ParentComponentDef.componentName), func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(ParentComponentDef.component, "", f.AsKubeAdmin.TektonController, &has.RetryOptions{Always: true, Retries: 2}, nil)).To(Succeed())
				pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(ParentComponentDef.component.GetName(), ParentComponentDef.component.Spec.Application, ParentComponentDef.component.GetNamespace(), "")
				Expect(err).ShouldNot(HaveOccurred())
				for _, result := range pr.Status.PipelineRunStatusFields.Results {
					if result.Name == "IMAGE_DIGEST" {
						parentFirstDigest = result.Value.StringVal
					}
				}
				Expect(parentFirstDigest).ShouldNot(BeEmpty())
			})
			// Now we have an initial image we create a dockerfile in the child that references this new image
			// This is the file that will be updated by the nudge
			It("create dockerfile and yaml manifest that references build and distribution repositorys", func() {

				component, err := f.AsKubeAdmin.HasController.GetComponent(ParentComponentDef.componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), "could not get component %s in the %s namespace", ParentComponentDef.componentName, testNamespace)

				annotations := component.GetAnnotations()
				imageRepoName, err := build.GetQuayImageName(annotations)
				Expect(err).ShouldNot(HaveOccurred())
				err = f.AsKubeAdmin.CommonController.Github.CreateRef(ChildComponentDef.repoName, ChildComponentDef.baseBranch, ChildComponentDef.baseRevision, ChildComponentDef.pacBranchName)
				Expect(err).ShouldNot(HaveOccurred())
				parentImageNameWithNoDigest = "quay.io/" + quayOrg + "/" + imageRepoName
				_, err = f.AsKubeAdmin.CommonController.Github.CreateFile(ChildComponentDef.repoName, "Dockerfile.tmp", "FROM "+parentImageNameWithNoDigest+"@"+parentFirstDigest+"\nRUN echo hello\n", ChildComponentDef.pacBranchName)
				Expect(err).ShouldNot(HaveOccurred())

				_, err = f.AsKubeAdmin.CommonController.Github.CreateFile(ChildComponentDef.repoName, "manifest.yaml", "image: "+distributionRepository+"@"+parentFirstDigest, ChildComponentDef.pacBranchName)
				Expect(err).ShouldNot(HaveOccurred())

				_, err = f.AsKubeAdmin.CommonController.Github.CreatePullRequest(ChildComponentDef.repoName, "update to build repo image", "update to build repo image", ChildComponentDef.pacBranchName, ChildComponentDef.componentBranch)
				Expect(err).ShouldNot(HaveOccurred())
				prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(ChildComponentDef.repoName)
				Expect(err).ShouldNot(HaveOccurred())

				prno := -1
				for _, pr := range prs {
					if pr.Head.GetRef() == ChildComponentDef.pacBranchName {
						prno = pr.GetNumber()
					}
				}
				Expect(prno).ShouldNot(Equal(-1))
				_, err = f.AsKubeAdmin.CommonController.Github.MergePullRequest(ChildComponentDef.repoName, prno)
				Expect(err).ShouldNot(HaveOccurred())

			})
			// This actually happens immediately, but we only need the PR number now
			It(fmt.Sprintf("should lead to a PaC PR creation for parent component %s", ParentComponentDef.componentName), func() {
				timeout = time.Second * 300
				interval := time.Second * 1

				Eventually(func() bool {
					prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(ParentComponentDef.repoName)
					Expect(err).ShouldNot(HaveOccurred())

					for _, pr := range prs {
						if pr.Head.GetRef() == ParentComponentDef.pacBranchName {
							prNumber = pr.GetNumber()
							return true
						}
					}
					return false
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for PaC PR (branch name '%s') to be created in %s repository", ParentComponentDef.pacBranchName, ParentComponentDef.repoName))
			})
			It(fmt.Sprintf("Merging the PaC PR should be successful for parent component %s", ParentComponentDef.componentName), func() {
				Eventually(func() error {
					mergeResult, err = f.AsKubeAdmin.CommonController.Github.MergePullRequest(ParentComponentDef.repoName, prNumber)
					return err
				}, time.Minute).Should(BeNil(), fmt.Sprintf("error when merging PaC pull request #%d in repo %s", prNumber, ParentComponentDef.repoName))

				mergeResultSha = mergeResult.GetSHA()
				GinkgoWriter.Printf("merged result sha: %s for PR #%d\n", mergeResultSha, prNumber)

			})
			// Now the PR is merged this will kick off another build. The result of this build is what we want to update in dockerfile we created
			It(fmt.Sprintf("PR merge triggers PAC PipelineRun for parent component %s", ParentComponentDef.componentName), func() {
				timeout = time.Minute * 5

				Eventually(func() error {
					pipelineRun, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(ParentComponentDef.componentName, applicationName, testNamespace, mergeResultSha)
					if err != nil {
						GinkgoWriter.Printf("Push PipelineRun has not been created yet for the component %s/%s\n", testNamespace, ParentComponentDef.componentName)
						return err
					}
					if !pipelineRun.HasStarted() {
						return fmt.Errorf("push pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, ParentComponentDef.componentName))
			})
			// Wait for this PR to be done and store the digest, we will need it to verify that the nudge was correct
			It(fmt.Sprintf("PAC PipelineRun for parent component %s is successful", ParentComponentDef.componentName), func() {
				pr := &pipeline.PipelineRun{}
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(ParentComponentDef.component, mergeResultSha, f.AsKubeAdmin.TektonController, &has.RetryOptions{Always: true, Retries: 2}, pr)).To(Succeed())

				for _, result := range pr.Status.PipelineRunStatusFields.Results {
					if result.Name == "IMAGE_DIGEST" {
						parentPostPacMergeDigest = result.Value.StringVal
					}
				}
				Expect(parentPostPacMergeDigest).ShouldNot(BeEmpty())
			})
			It(fmt.Sprintf("should lead to a nudge PR creation for child component %s", ChildComponentDef.componentName), func() {
				timeout = time.Minute * 20
				interval := time.Second * 1

				Eventually(func() bool {
					prs, err := f.AsKubeAdmin.CommonController.Github.ListPullRequests(componentDependenciesChildRepoName)
					Expect(err).ShouldNot(HaveOccurred())

					for _, pr := range prs {
						if strings.Contains(pr.Head.GetRef(), ParentComponentDef.componentName) {
							prNumber = pr.GetNumber()
							return true
						}
					}
					return false
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("timed out when waiting for component nudge PR to be created in %s repository", componentDependenciesChildRepoName))
			})
			It(fmt.Sprintf("merging the PR should be successful for child component %s", ChildComponentDef.componentName), func() {
				Eventually(func() error {
					mergeResult, err = f.AsKubeAdmin.CommonController.Github.MergePullRequest(componentDependenciesChildRepoName, prNumber)
					return err
				}, time.Minute).Should(BeNil(), fmt.Sprintf("error when merging nudge pull request #%d in repo %s", prNumber, componentDependenciesChildRepoName))

				mergeResultSha = mergeResult.GetSHA()
				GinkgoWriter.Printf("merged result sha: %s for PR #%d\n", mergeResultSha, prNumber)

			})
			// Now the nudge has been merged we verify the dockerfile is what we expected
			It("Verify the nudge updated the contents", func() {

				GinkgoWriter.Printf("Verifying Dockerfile.tmp updated to sha %s", parentPostPacMergeDigest)
				component, err := f.AsKubeAdmin.HasController.GetComponent(ParentComponentDef.componentName, testNamespace)
				Expect(err).ShouldNot(HaveOccurred(), "could not get component %s in the %s namespace", ParentComponentDef.componentName, testNamespace)

				annotations := component.GetAnnotations()
				imageRepoName, err := build.GetQuayImageName(annotations)
				Expect(err).ShouldNot(HaveOccurred())
				contents, err := f.AsKubeAdmin.CommonController.Github.GetFile(ChildComponentDef.repoName, "Dockerfile.tmp", ChildComponentDef.componentBranch)
				Expect(err).ShouldNot(HaveOccurred())
				content, err := contents.GetContent()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(content).Should(Equal("FROM quay.io/" + quayOrg + "/" + imageRepoName + "@" + parentPostPacMergeDigest + "\nRUN echo hello\n"))

				contents, err = f.AsKubeAdmin.CommonController.Github.GetFile(ChildComponentDef.repoName, "manifest.yaml", ChildComponentDef.componentBranch)
				Expect(err).ShouldNot(HaveOccurred())
				content, err = contents.GetContent()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(content).Should(Equal("image: " + distributionRepository + "@" + parentPostPacMergeDigest))

			})
		})

	})

})

func createBuildSecret(f *framework.Framework, secretName string, annotations map[string]string, token string) error {
	buildSecret := v1.Secret{}
	buildSecret.Name = secretName
	buildSecret.Labels = map[string]string{
		"appstudio.redhat.com/credentials": "scm",
		"appstudio.redhat.com/scm.host":    "github.com",
	}
	if annotations != nil {
		buildSecret.Annotations = annotations
	}
	buildSecret.Type = "kubernetes.io/basic-auth"
	buildSecret.StringData = map[string]string{
		"password": token,
	}
	_, err := f.AsKubeAdmin.CommonController.CreateSecret(f.UserNamespace, &buildSecret)
	if err != nil {
		return fmt.Errorf("error creating build secret: %v", err)
	}
	return nil
}

func cleanupWebhooks(f *framework.Framework, repoName string) {
	hooks, err := f.AsKubeAdmin.CommonController.Github.ListRepoWebhooks(repoName)
	Expect(err).NotTo(HaveOccurred())
	for _, h := range hooks {
		hookUrl := h.Config["url"].(string)
		if strings.Contains(hookUrl, f.ClusterAppDomain) {
			GinkgoWriter.Printf("removing webhook URL: %s\n", hookUrl)
			Expect(f.AsKubeAdmin.CommonController.Github.DeleteWebhook(repoName, h.GetID())).To(Succeed())
			break
		}
	}
}
