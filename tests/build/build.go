package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/utils/pointer"

	"github.com/google/go-github/v44/github"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/has"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/build"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/tekton"

	"github.com/devfile/library/v2/pkg/util"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	buildservice "github.com/redhat-appstudio/build-service/api/v1alpha1"
	"github.com/redhat-appstudio/build-service/controllers"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	v1 "k8s.io/api/core/v1"
)

var _ = framework.BuildSuiteDescribe("Build service E2E tests", Label("build", "HACBS"), func() {

	var f *framework.Framework
	AfterEach(framework.ReportFailure(&f))

	var err error
	var osConsoleHost string
	defer GinkgoRecover()

	Describe("test PaC component build", Ordered, Label("github-webhook", "pac-build", "pipeline", "image-controller"), func() {
		var applicationName, componentName, componentBaseBranchName, pacBranchName, testNamespace, defaultBranchTestComponentName, imageRepoName, robotAccountName string
		var component *appservice.Component

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

			consoleRoute, err := f.AsKubeAdmin.CommonController.GetOpenshiftRoute("console", "openshift-console")
			Expect(err).ShouldNot(HaveOccurred())
			osConsoleHost = consoleRoute.Spec.Host

			if utils.IsPrivateHostname(osConsoleHost) {
				Skip("Using private cluster (not reachable from Github), skipping...")
			}

			supports, err := build.DoesQuayOrgSupportPrivateRepo()
			Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("error while checking if quay org supports private repo: %+v", err))
			if !supports {
				Skip("Quay org does not support private quay repository creation, please add support for private repo creation before running this test")
			}
			Expect(err).ShouldNot(HaveOccurred())

			applicationName = fmt.Sprintf("build-suite-test-application-%s", util.GenerateRandomString(4))
			app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
				Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
			)

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

		})

		validateChecks := func() {
			var checkRun *github.CheckRun
			timeout = time.Minute * 15
			interval = time.Second * 10

			Eventually(func() *github.CheckRun {
				checkRuns, err := f.AsKubeAdmin.CommonController.Github.ListCheckRuns(helloWorldComponentGitSourceRepoName, prHeadSha)
				Expect(err).ShouldNot(HaveOccurred())
				for _, cr := range checkRuns {
					if strings.Contains(cr.GetDetailsURL(), osConsoleHost) {
						checkRun = cr
						return cr
					}
				}
				return nil
			}, timeout, interval).ShouldNot(BeNil(), fmt.Sprintf("timed out when waiting for the PaC Check run with `Details URL` field containing %s to appear in the Component repo %s in PR #%d", osConsoleHost, helloWorldComponentGitSourceRepoName, prNumber))

			Eventually(func() string {
				checkRun, err = f.AsKubeAdmin.CommonController.Github.GetCheckRun(helloWorldComponentGitSourceRepoName, checkRun.GetID())
				Expect(err).ShouldNot(HaveOccurred())
				return checkRun.GetStatus()
			}, timeout, interval).Should(Equal("completed"), fmt.Sprintf("timed out when waiting for the PaC Check suite status to be 'completed' in the Component repo %s in PR #%d", helloWorldComponentGitSourceRepoName, prNumber))
			Expect(checkRun.GetConclusion()).To(Equal("success"), fmt.Sprintf("the initial PR %d in %s repo doesn't contain the info about successful pipelinerun", prNumber, helloWorldComponentGitSourceRepoName))
		}

		When("a new component without specified branch is created and with visibility private", Label("pac-custom-default-branch"), func() {
			BeforeAll(func() {
				componentObj := appservice.ComponentSpec{
					ComponentName: defaultBranchTestComponentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:      helloWorldComponentGitSourceURL,
								Revision: "",
							},
						},
					},
				}
				_, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPrivateRepo))
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
					pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(defaultBranchTestComponentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, defaultBranchTestComponentName)
						return err
					}
					if !pr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
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
				var pr *v1beta1.PipelineRun
				Eventually(func() error {
					pr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(defaultBranchTestComponentName, applicationName, testNamespace, "")
					if err == nil {
						return fmt.Errorf("pipelinerun %s/%s is not removed yet", pr.GetNamespace(), pr.GetName())
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

		When("a new Component with specified custom branch is created", Label("custom-branch"), func() {
			var outputImage string
			BeforeAll(func() {
				componentObj := appservice.ComponentSpec{
					ComponentName: componentName,
					Application:   applicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:      helloWorldComponentGitSourceURL,
								Revision: componentBaseBranchName,
							},
						},
					},
				}
				// Create a component with Git Source URL, a specified git branch and marking delete-repo=true
				component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo))
				Expect(err).ShouldNot(HaveOccurred())
			})
			It("triggers a PipelineRun", func() {
				timeout = time.Second * 600
				interval = time.Second * 1
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
					f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
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
				pipelineRun, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())

				for _, p := range pipelineRun.Spec.Params {
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
				pipelineRun, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).ShouldNot(HaveOccurred())

				image, err := build.ImageFromPipelineRun(pipelineRun)
				Expect(err).ShouldNot(HaveOccurred())

				labels := image.Config.Config.Labels
				Expect(labels).ToNot(BeEmpty())

				expiration, ok := labels["quay.expires-after"]
				Expect(ok).To(BeTrue())
				Expect(expiration).To(Equal(utils.GetEnv(constants.IMAGE_TAG_EXPIRATION_ENV, constants.DefaultImageTagExpiration)))
			})
			It("eventually leads to the PipelineRun status report at Checks tab", func() {
				validateChecks()
			})
		})

		When("the PaC init branch is updated", func() {
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
					pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, createdFileSHA)
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !pr.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
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
					f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
			})
			It("eventually leads to another update of a PR about the PipelineRun status report at Checks tab", func() {
				validateChecks()
			})
		})

		When("the PaC init branch is merged", func() {
			var mergeResult *github.PullRequestMergeResult
			var mergeResultSha string
			var pipelineRun *v1beta1.PipelineRun

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
					pipelineRun, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, mergeResultSha)
					if err != nil {
						GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !pipelineRun.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
					}
					return nil
				}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
			})

			It("pipelineRun should eventually finish", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component,
					mergeResultSha, f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
			})

			It("image does not have expiration set", func() {
				image, err := build.ImageFromPipelineRun(pipelineRun)
				Expect(err).ShouldNot(HaveOccurred())

				labels := image.Config.Config.Labels
				Expect(labels).ToNot(BeEmpty())

				expiration, ok := labels["quay.expires-after"]
				Expect(ok).To(BeFalse())
				Expect(expiration).To(BeEmpty())
			})

			It("After updating image visibility to private, it should not trigger another PipelineRun", func() {
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.SetComponentAnnotation(componentName, controllers.ImageRepoGenerateAnnotationName, constants.ImageControllerAnnotationRequestPrivateRepo[controllers.ImageRepoGenerateAnnotationName], testNamespace)).To(Succeed())

				Consistently(func() bool {
					componentPipelineRun, _ := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
					return componentPipelineRun == nil
				}, time.Minute, constants.PipelineRunPollingInterval).Should(BeTrue(), fmt.Sprintf("expected no PipelineRun to be triggered for the component %s in %s namespace", componentName, testNamespace))
			})
			It("image repo is updated to private", func() {
				isPublic, err := build.IsImageRepoPublic(imageRepoName)
				Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed while checking if the image repo %s is private", imageRepoName))
				Expect(isPublic).To(BeFalse(), "Expected image repo to changed to private, but it is public")
			})
			It("retrigger the pipeline manually", func() {
				Expect(f.AsKubeAdmin.HasController.SetComponentAnnotation(componentName, controllers.BuildRequestAnnotationName, controllers.BuildRequestTriggerPaCBuildAnnotationValue, testNamespace)).To(Succeed())
				// Check the pipelinerun is triggered
				Eventually(func() error {
					pipelineRun, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
					if err != nil {
						GinkgoWriter.Printf("PipelineRun is not been retriggered yet for the component %s/%s\n", testNamespace, componentName)
						return err
					}
					if !pipelineRun.HasStarted() {
						return fmt.Errorf("pipelinerun %s/%s hasn't been started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
					}
					return nil
				}, 2*time.Minute, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to retrigger for the component %s/%s", testNamespace, componentName))
			})
			It("retriggered pipelineRun should eventually finish", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "", 2, f.AsKubeAdmin.TektonController)).To(Succeed())
			})
		})

		When("the component is removed and recreated (with the same name in the same namespace)", func() {
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
								URL:      helloWorldComponentGitSourceURL,
								Revision: componentBaseBranchName,
							},
						},
					},
				}
				_, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo))
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

			consoleRoute, err := f.AsKubeAdmin.CommonController.GetOpenshiftRoute("console", "openshift-console")
			Expect(err).ShouldNot(HaveOccurred())
			osConsoleHost = consoleRoute.Spec.Host

			if utils.IsPrivateHostname(osConsoleHost) {
				Skip("Using private cluster (not reachable from Github), skipping...")
			}

			applicationName = fmt.Sprintf("build-suite-positive-mc-%s", util.GenerateRandomString(4))
			app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
				Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
			)

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
									URL:      multiComponentGitSourceURL,
									Revision: multiComponentBaseBranchName,
									Context:  contextDir,
								},
							},
						},
					}
					component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo))
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
						f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true})).To(Succeed())
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
				app, err := f.AsKubeAdmin.HasController.CreateApplication(appName, namespace)
				Expect(err).NotTo(HaveOccurred())
				Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
					Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
				)

				compName = fmt.Sprintf("%s-%s", multiComponentContextDirs[0], util.GenerateRandomString(6))

				componentObj := appservice.ComponentSpec{
					ComponentName: compName,
					Application:   appName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:      multiComponentGitSourceURL,
								Revision: multiComponentBaseBranchName,
								Context:  multiComponentContextDirs[0],
							},
						},
					},
				}
				_, err = fw.AsKubeAdmin.HasController.CreateComponent(componentObj, namespace, "", "", appName, false, utils.MergeMaps(constants.ComponentPaCRequestAnnotation, constants.ImageControllerAnnotationRequestPublicRepo))
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
			app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
				Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
			)

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
								URL:      annotationsTestGitSourceURL,
								Revision: annotationsTestRevision,
							},
						},
					},
				}

				component, err = f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, false, nil)
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
					if !strings.Contains(buildStatus.Message, fmt.Sprintf("unexpected build request: %s", invalidAnnotation)) {
						return fmt.Errorf("build status message is not as expected, got %s", buildStatus.Message)
					}
					return nil
				}, time.Minute*1, 2*time.Second).Should(Succeed(), "failed while checking build status message is correct after setting invalid annotations")
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

			app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
				Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
			)

			componentName = fmt.Sprintf("build-suite-test-component-image-source-%s", util.GenerateRandomString(6))
			outputContainerImage := ""
			timeout = time.Second * 10
			// Create a component with containerImageSource being defined
			component := appservice.ComponentSpec{
				ComponentName:  fmt.Sprintf("build-suite-test-component-image-source-%s", util.GenerateRandomString(6)),
				ContainerImage: containerImageSource,
			}
			_, err = f.AsKubeAdmin.HasController.CreateComponent(component, testNamespace, outputContainerImage, "", applicationName, true, map[string]string{})
			Expect(err).ShouldNot(HaveOccurred())

			// collect Build ResourceQuota metrics (temporary)
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, appstudioCrdsBuild)
			Expect(err).NotTo(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, computeBuild)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			// collect Build ResourceQuota metrics (temporary)
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, appstudioCrdsBuild)
			Expect(err).NotTo(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, computeBuild)
			Expect(err).NotTo(HaveOccurred())
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

	Describe("PLNSRVCE-799 - test pipeline selector", Label("pipeline-selector"), Ordered, func() {
		var timeout time.Duration
		var componentName, applicationName, testNamespace string
		var expectedAdditionalPipelineParam buildservice.PipelineParam
		var pr *v1beta1.PipelineRun

		BeforeAll(func() {
			f, err = framework.NewFramework(utils.GetGeneratedNamespace("build-e2e"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace
			applicationName = fmt.Sprintf("test-app-%s", util.GenerateRandomString(4))

			app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
				Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s: %+v", app.Name, app.Namespace, err),
			)

			componentName = "build-suite-test-bundle-overriding"

			expectedAdditionalPipelineParam = buildservice.PipelineParam{
				Name:  "test-custom-param-name",
				Value: "test-custom-param-value",
			}

			timeout = time.Second * 600

			// collect Build ResourceQuota metrics (temporary)
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, appstudioCrdsBuild)
			Expect(err).NotTo(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, computeBuild)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			// collect Build ResourceQuota metrics (temporary)
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, appstudioCrdsBuild)
			Expect(err).NotTo(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, computeBuild)
			Expect(err).NotTo(HaveOccurred())

			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
				Expect(f.SandboxController.DeleteUserSignup(f.UserName)).To(BeTrue())
			}
		})

		It("a specific Pipeline bundle should be used and additional pipeline params should be added to the PipelineRun if all WhenConditions match", func() {
			// using cdq since git ref is not known
			cdq, err := f.AsKubeAdmin.HasController.CreateComponentDetectionQuery(componentName, testNamespace, helloWorldComponentGitSourceURL, "", "", "", false)
			Expect(err).NotTo(HaveOccurred())
			Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

			for _, compDetected := range cdq.Status.ComponentDetected {
				// Since we only know the component name after cdq creation,
				// BuildPipelineSelector should be created before component creation and after cdq creation
				ps := &buildservice.BuildPipelineSelector{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "build-pipeline-selector",
						Namespace: testNamespace,
					},
					Spec: buildservice.BuildPipelineSelectorSpec{Selectors: []buildservice.PipelineSelector{
						{
							Name:           "user-custom-selector",
							PipelineRef:    *tekton.NewBundleResolverPipelineRef("docker-build", dummyPipelineBundleRef),
							PipelineParams: []buildservice.PipelineParam{expectedAdditionalPipelineParam},
							WhenConditions: buildservice.WhenCondition{
								ProjectType:        "hello-world",
								DockerfileRequired: pointer.Bool(true),
								ComponentName:      compDetected.ComponentStub.ComponentName,
								Annotations:        map[string]string{"skip-initial-checks": "true"},
								Labels:             constants.ComponentDefaultLabel,
							},
						},
					}},
				}

				Expect(f.AsKubeAdmin.CommonController.KubeRest().Create(context.Background(), ps)).To(Succeed())
				c, err := f.AsKubeAdmin.HasController.CreateComponent(compDetected.ComponentStub, testNamespace, "", "", applicationName, true, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				componentName = c.Name
			}

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
			Expect(pr.Spec.PipelineRef.Params).To(ContainElement(v1beta1.Param{
				Name:  "bundle",
				Value: v1beta1.ParamValue{StringVal: dummyPipelineBundleRef, Type: "string"}},
			))
			Expect(pr.Spec.Params).To(ContainElement(v1beta1.Param{
				Name:  expectedAdditionalPipelineParam.Name,
				Value: v1beta1.ParamValue{StringVal: expectedAdditionalPipelineParam.Value, Type: "string"}},
			))
		})

		It("default Pipeline bundle should be used and no additional Pipeline params should be added to the PipelineRun if one of the WhenConditions does not match", func() {
			notMatchingComponentName := componentName + util.GenerateRandomString(6)
			// using cdq since git ref is not known
			cdq, err := f.AsKubeAdmin.HasController.CreateComponentDetectionQuery(notMatchingComponentName, testNamespace, helloWorldComponentGitSourceURL, "", "", "", false)
			Expect(err).NotTo(HaveOccurred())
			Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

			for _, compDetected := range cdq.Status.ComponentDetected {
				c, err := f.AsKubeAdmin.HasController.CreateComponent(compDetected.ComponentStub, testNamespace, "", "", applicationName, true, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				notMatchingComponentName = c.Name
			}

			Eventually(func() error {
				pr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(notMatchingComponentName, applicationName, testNamespace, "")
				if err != nil {
					GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s\n", testNamespace, notMatchingComponentName)
					return err
				}
				if !pr.HasStarted() {
					return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
				}
				return err
			}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, notMatchingComponentName))

			pr, err = f.AsKubeAdmin.HasController.GetComponentPipelineRun(notMatchingComponentName, applicationName, testNamespace, "")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(tekton.GetBundleRef(pr.Spec.PipelineRef)).ToNot(Equal(dummyPipelineBundleRef)) //nolint:all
			Expect(pr.Spec.Params).ToNot(ContainElement(v1beta1.Param{
				Name:  expectedAdditionalPipelineParam.Name,
				Value: v1beta1.ParamValue{StringVal: expectedAdditionalPipelineParam.Value, Type: "string"}},
			))
		})
	})

	Describe("A secret with dummy quay.io credentials is created in the testing namespace", Ordered, func() {

		var applicationName, componentName, testNamespace string
		var timeout time.Duration
		var err error
		var pr *v1beta1.PipelineRun

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

			app, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(utils.WaitUntil(f.AsKubeAdmin.HasController.ApplicationGitopsRepoExists(app.Status.Devfile), 30*time.Second)).To(
				Succeed(), fmt.Sprintf("timed out waiting for gitops content to be created for app %s in namespace %s", app.Name, app.Namespace),
			)
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
			// using cdq since git ref is not known
			cdq, err := f.AsKubeAdmin.HasController.CreateComponentDetectionQuery(componentName, testNamespace, helloWorldComponentGitSourceURL, "", "", "", false)
			Expect(err).NotTo(HaveOccurred())
			Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

			for _, compDetected := range cdq.Status.ComponentDetected {
				c, err := f.AsKubeAdmin.HasController.CreateComponent(compDetected.ComponentStub, testNamespace, "", "", applicationName, true, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				componentName = c.Name
			}

			// collect Build ResourceQuota metrics (temporary)
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, appstudioCrdsBuild)
			Expect(err).NotTo(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, computeBuild)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			// collect Build ResourceQuota metrics (temporary)
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, appstudioCrdsBuild)
			Expect(err).NotTo(HaveOccurred())
			err = f.AsKubeAdmin.CommonController.GetResourceQuotaInfo("build", testNamespace, computeBuild)
			Expect(err).NotTo(HaveOccurred())

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
})
