package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/devfile/library/pkg/util"
	"k8s.io/apimachinery/pkg/runtime"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"k8s.io/klog/v2"

	"sigs.k8s.io/yaml"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	remoteimg "github.com/google/go-containerregistry/pkg/v1/remote"
	gh "github.com/google/go-github/v44/github"
	"github.com/magefile/mage/sh"
	"github.com/redhat-appstudio/e2e-tests/magefiles/installation"
	"github.com/redhat-appstudio/e2e-tests/pkg/apis/github"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/image-controller/pkg/quay"
	tektonapi "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
)

var (
	requiredBinaries = []string{"jq", "kubectl", "oc", "yq", "git", "helm"}
	artifactDir      = utils.GetEnv("ARTIFACT_DIR", ".")
	openshiftJobSpec = &OpenshiftJobSpec{}
	pr               = &PullRequestMetadata{}
	jobName          = utils.GetEnv("JOB_NAME", "")
	// can be periodic, presubmit or postsubmit
	jobType                    = utils.GetEnv("JOB_TYPE", "")
	reposToDeleteDefaultRegexp = "jvm-build|e2e-dotnet|build-suite|e2e|pet-clinic-e2e|test-app|e2e-quayio|petclinic|test-app|integ-app|^dockerfile-|new-|^python|my-app|^test-|^multi-component"
	repositoriesWithWebhooks   = []string{"devfile-sample-hello-world", "hacbs-test-project"}
)

func (CI) parseJobSpec() error {
	jobSpecEnvVarData := os.Getenv("JOB_SPEC")

	if err := json.Unmarshal([]byte(jobSpecEnvVarData), openshiftJobSpec); err != nil {
		return fmt.Errorf("error when parsing openshift job spec data: %v", err)
	}
	return nil
}

func (ci CI) init() error {
	var err error

	if jobType == "periodic" || strings.Contains(jobName, "rehearse") {
		return nil
	}

	if err = ci.parseJobSpec(); err != nil {
		return err
	}

	pr.Organization = openshiftJobSpec.Refs.Organization
	pr.RepoName = openshiftJobSpec.Refs.Repo
	pr.CommitSHA = openshiftJobSpec.Refs.Pulls[0].SHA
	pr.Number = openshiftJobSpec.Refs.Pulls[0].Number

	prUrl := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", pr.Organization, pr.RepoName, pr.Number)
	pr.RemoteName, pr.BranchName, err = getRemoteAndBranchNameFromPRLink(prUrl)
	if err != nil {
		return err
	}

	return nil
}

func (ci CI) PrepareE2EBranch() error {
	if jobType == "periodic" || strings.Contains(jobName, "rehearse") {
		return nil
	}

	if err := ci.init(); err != nil {
		return err
	}

	if openshiftJobSpec.Refs.Repo == "e2e-tests" {
		if err := gitCheckoutRemoteBranch(pr.RemoteName, pr.CommitSHA); err != nil {
			return err
		}
	} else {
		if ci.isPRPairingRequired("e2e-tests") {
			if err := gitCheckoutRemoteBranch(pr.RemoteName, pr.BranchName); err != nil {
				return err
			}
		}
	}

	return nil
}

func (Local) PrepareCluster() error {
	if err := PreflightChecks(); err != nil {
		return fmt.Errorf("error when running preflight checks: %v", err)
	}
	if err := BootstrapCluster(); err != nil {
		return fmt.Errorf("error when bootstrapping cluster: %v", err)
	}

	return nil
}

func (Local) TestE2E() error {
	return RunE2ETests()
}

// Deletes autogenerated repositories from redhat-appstudio-qe Github org.
// Env vars to configure this target: REPO_REGEX (optional), DRY_RUN (optional) - defaults to false
// Remove all repos which with 1 day lifetime. By default will delete gitops repositories from redhat-appstudio-qe
func (Local) CleanupGithubOrg() error {
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		return fmt.Errorf("env var GITHUB_TOKEN is not set")
	}
	dryRun, err := strconv.ParseBool(utils.GetEnv("DRY_RUN", "true"))
	if err != nil {
		return fmt.Errorf("unable to parse DRY_RUN env var\n\t%s", err)
	}

	// Get all repos
	githubOrgName := utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")
	ghClient, err := github.NewGithubClient(githubToken, githubOrgName)
	if err != nil {
		return err
	}
	repos, err := ghClient.GetAllRepositories()
	if err != nil {
		return err
	}
	var reposToDelete []*gh.Repository

	// Filter repos by regex & time check
	r, err := regexp.Compile(utils.GetEnv("REPO_REGEX", reposToDeleteDefaultRegexp))
	if err != nil {
		return fmt.Errorf("unable to compile regex: %s", err)
	}
	for _, repo := range repos {
		// Add only repos older than 24 hours
		dayDuration, _ := time.ParseDuration("24h")
		if time.Since(repo.GetCreatedAt().Time) > dayDuration {
			// Add only repos matching the regex
			if r.MatchString(*repo.Name) {
				reposToDelete = append(reposToDelete, repo)
			}
		}
	}

	if dryRun {
		klog.Info("Dry run enabled. Listing repositories that would be deleted:")
	}

	// Delete repos
	for _, repo := range reposToDelete {
		if dryRun {
			klog.Infof("\t%s", repo.GetName())
		} else {
			err := ghClient.DeleteRepository(repo)
			if err != nil {
				klog.Warningf("error deleting repository: %s\n", err)
			}
		}
	}
	if dryRun {
		klog.Info("If you really want to delete these repositories, run `DRY_RUN=false [REGEXP=<regexp>] mage local:cleanupGithubOrg`")
	}
	return nil
}

// Deletes Quay repos and robot accounts older than 24 hours with prefixes `has-e2e` and `e2e-demos`, uses env vars DEFAULT_QUAY_ORG and DEFAULT_QUAY_ORG_TOKEN
func (Local) CleanupQuayReposAndRobots() error {
	quayOrgToken := os.Getenv("DEFAULT_QUAY_ORG_TOKEN")
	if quayOrgToken == "" {
		return fmt.Errorf("DEFAULT_QUAY_ORG_TOKEN env var was not found")
	}
	quayOrg := utils.GetEnv("DEFAULT_QUAY_ORG", "redhat-appstudio-qe")

	quayClient := quay.NewQuayClient(&http.Client{Transport: &http.Transport{}}, quayOrgToken, "https://quay.io/api/v1")
	return cleanupQuayReposAndRobots(&quayClient, quayOrg)
}

// Deletes Quay Tags older than 7 days in `test-images` repository
func (Local) CleanupQuayTags() error {
	quayOrgToken := os.Getenv("DEFAULT_QUAY_ORG_TOKEN")
	if quayOrgToken == "" {
		return fmt.Errorf("DEFAULT_QUAY_ORG_TOKEN env var was not found")
	}
	quayOrg := utils.GetEnv("DEFAULT_QUAY_ORG", "redhat-appstudio-qe")

	quayClient := quay.NewQuayClient(&http.Client{Transport: &http.Transport{}}, quayOrgToken, "https://quay.io/api/v1")
	return cleanupQuayTags(&quayClient, quayOrg, "test-images")
}

func (ci CI) TestE2E() error {
	var testFailure bool

	if err := ci.init(); err != nil {
		return fmt.Errorf("error when running ci init: %v", err)
	}

	if err := PreflightChecks(); err != nil {
		return fmt.Errorf("error when running preflight checks: %v", err)
	}

	if err := ci.setRequiredEnvVars(); err != nil {
		return fmt.Errorf("error when setting up required env vars: %v", err)
	}

	if err := retry(BootstrapCluster, 2, 10*time.Second); err != nil {
		return fmt.Errorf("error when bootstrapping cluster: %v", err)
	}

	if err := RunE2ETests(); err != nil {
		testFailure = true
	}

	if err := ci.sendWebhook(); err != nil {
		klog.Infof("error when sending webhook: %v", err)
	}

	if testFailure {
		return fmt.Errorf("error when running e2e tests - see the log above for more details")
	}

	return nil
}

func RunE2ETests() error {
	cwd, _ := os.Getwd()

	// added --output-interceptor-mode=none to mitigate RHTAPBUGS-34
	return sh.RunV("ginkgo", "-p", "--output-interceptor-mode=none", "--timeout=90m", fmt.Sprintf("--output-dir=%s", artifactDir), "--junit-report=e2e-report.xml", "--label-filter=$E2E_TEST_SUITE_LABEL", "./cmd", "--", fmt.Sprintf("--config-suites=%s/tests/e2e-demos/config/default.yaml", cwd), "--generate-rppreproc-report=true", fmt.Sprintf("--rp-preproc-dir=%s", artifactDir))
}

func PreflightChecks() error {
	requiredEnv := []string{
		"GITHUB_TOKEN",
		"QUAY_TOKEN",
		"DEFAULT_QUAY_ORG",
		"DEFAULT_QUAY_ORG_TOKEN",
	}
	missingEnv := []string{}
	for _, env := range requiredEnv {
		if os.Getenv(env) == "" {
			missingEnv = append(missingEnv, env)
		}
	}
	if len(missingEnv) != 0 {
		return fmt.Errorf("required env vars containing secrets (%s) not defined or empty", strings.Join(missingEnv, ","))
	}

	for _, binaryName := range requiredBinaries {
		if err := sh.Run("which", binaryName); err != nil {
			return fmt.Errorf("binary %s not found in PATH - please install it first", binaryName)
		}
	}

	if err := sh.RunV("go", "install", "-mod=mod", "github.com/onsi/ginkgo/v2/ginkgo"); err != nil {
		return err
	}

	return nil
}

func (ci CI) setRequiredEnvVars() error {

	if strings.Contains(jobName, "hacbs-e2e-periodic") {
		os.Setenv("E2E_TEST_SUITE_LABEL", "HACBS")
		return nil
	} else if strings.Contains(jobName, "appstudio-e2e-deployment-periodic") {
		os.Setenv("E2E_TEST_SUITE_LABEL", "!HACBS")
		return nil
	}

	if openshiftJobSpec.Refs.Repo != "e2e-tests" {

		if strings.Contains(jobName, "-service") || strings.Contains(jobName, "image-controller") {
			var envVarPrefix, imageTagSuffix, testSuiteLabel string
			sp := strings.Split(os.Getenv("COMPONENT_IMAGE"), "@")

			switch {
			case strings.Contains(jobName, "application-service"):
				envVarPrefix = "HAS"
				imageTagSuffix = "has-image"
				testSuiteLabel = "has,e2e-demo,byoc"
			case strings.Contains(jobName, "release-service"):
				envVarPrefix = "RELEASE_SERVICE"
				imageTagSuffix = "release-service-image"
				testSuiteLabel = "release"
			case strings.Contains(jobName, "integration-service"):
				envVarPrefix = "INTEGRATION_SERVICE"
				imageTagSuffix = "integration-service-image"
				testSuiteLabel = "integration-service"
			case strings.Contains(jobName, "jvm-build-service"):
				envVarPrefix = "JVM_BUILD_SERVICE"
				imageTagSuffix = "jvm-build-service-image"
				testSuiteLabel = "jvm-build"
				// Since CI requires to have default values for dependency images
				// (https://github.com/openshift/release/blob/master/ci-operator/step-registry/redhat-appstudio/e2e/redhat-appstudio-e2e-ref.yaml#L15)
				// we cannot let these env vars to have identical names in CI as those env vars used in tests
				// e.g. JVM_BUILD_SERVICE_REQPROCESSOR_IMAGE, otherwise those images they are referencing wouldn't
				// be always relevant for tests and tests would be failing
				os.Setenv(fmt.Sprintf("%s_REQPROCESSOR_IMAGE", envVarPrefix), os.Getenv("CI_JBS_REQPROCESSOR_IMAGE"))
				os.Setenv(fmt.Sprintf("%s_CACHE_IMAGE", envVarPrefix), os.Getenv("CI_JBS_CACHE_IMAGE"))

				klog.Infof("going to override default Tekton bundle for the purpose of testing jvm-build-service PR")
				var err error
				var defaultBundleRef string
				var tektonObj runtime.Object

				tag := fmt.Sprintf("%d-%s", time.Now().Unix(), util.GenerateRandomString(4))
				var newS2iJavaTaskRef, _ = name.ParseReference(fmt.Sprintf("%s:task-bundle-%s", constants.DefaultImagePushRepo, tag))
				var newJavaBuilderPipelineRef, _ = name.ParseReference(fmt.Sprintf("%s:pipeline-bundle-%s", constants.DefaultImagePushRepo, tag))
				var newReqprocessorImage = os.Getenv("JVM_BUILD_SERVICE_REQPROCESSOR_IMAGE")
				var newTaskYaml, newPipelineYaml []byte

				if err = utils.CreateDockerConfigFile(os.Getenv("QUAY_TOKEN")); err != nil {
					return fmt.Errorf("failed to create docker config file: %+v", err)
				}
				if defaultBundleRef, err = utils.GetDefaultPipelineBundleRef(constants.BuildPipelineSelectorYamlURL, "Java"); err != nil {
					return fmt.Errorf("failed to get the pipeline bundle ref: %+v", err)
				}
				if tektonObj, err = utils.ExtractTektonObjectFromBundle(defaultBundleRef, "pipeline", "java-builder"); err != nil {
					return fmt.Errorf("failed to extract the Tekton Pipeline from bundle: %+v", err)
				}
				javaPipelineObj := tektonObj.(tektonapi.PipelineObject)

				var currentS2iJavaTaskRef string
				for _, t := range javaPipelineObj.PipelineSpec().Tasks {
					if t.TaskRef.Name == "s2i-java" {
						currentS2iJavaTaskRef = t.TaskRef.Bundle
						t.TaskRef.Bundle = newS2iJavaTaskRef.String()
					}
				}
				if tektonObj, err = utils.ExtractTektonObjectFromBundle(currentS2iJavaTaskRef, "task", "s2i-java"); err != nil {
					return fmt.Errorf("failed to extract the Tekton Task from bundle: %+v", err)
				}
				taskObj := tektonObj.(tektonapi.TaskObject)

				for i, s := range taskObj.TaskSpec().Steps {
					if s.Name == "analyse-dependencies-java-sbom" {
						taskObj.TaskSpec().Steps[i].Image = newReqprocessorImage
					}
				}

				if newTaskYaml, err = yaml.Marshal(taskObj); err != nil {
					return fmt.Errorf("error when marshalling a new task to YAML: %v", err)
				}
				if newPipelineYaml, err = yaml.Marshal(javaPipelineObj); err != nil {
					return fmt.Errorf("error when marshalling a new pipeline to YAML: %v", err)
				}

				keychain := authn.NewMultiKeychain(authn.DefaultKeychain)
				authOption := remoteimg.WithAuthFromKeychain(keychain)

				if err = utils.BuildAndPushTektonBundle(newTaskYaml, newS2iJavaTaskRef, authOption); err != nil {
					return fmt.Errorf("error when building/pushing a tekton task bundle: %v", err)
				}
				if err = utils.BuildAndPushTektonBundle(newPipelineYaml, newJavaBuilderPipelineRef, authOption); err != nil {
					return fmt.Errorf("error when building/pushing a tekton pipeline bundle: %v", err)
				}
				os.Setenv(constants.CUSTOM_JAVA_PIPELINE_BUILD_BUNDLE_ENV, newJavaBuilderPipelineRef.String())
			case strings.Contains(jobName, "build-service"):
				envVarPrefix = "BUILD_SERVICE"
				imageTagSuffix = "build-service-image"
				testSuiteLabel = "build"
			case strings.Contains(jobName, "image-controller"):
				envVarPrefix = "IMAGE_CONTROLLER"
				imageTagSuffix = "image-controller-image"
				testSuiteLabel = "pac-build"
			}

			os.Setenv(fmt.Sprintf("%s_IMAGE_REPO", envVarPrefix), sp[0])
			os.Setenv(fmt.Sprintf("%s_IMAGE_TAG", envVarPrefix), fmt.Sprintf("redhat-appstudio-%s", imageTagSuffix))
			// "rehearse" jobs metadata are not relevant for testing
			if !strings.Contains(jobName, "rehearse") {
				os.Setenv(fmt.Sprintf("%s_PR_OWNER", envVarPrefix), pr.RemoteName)
				os.Setenv(fmt.Sprintf("%s_PR_SHA", envVarPrefix), pr.CommitSHA)
			}

			os.Setenv("E2E_TEST_SUITE_LABEL", testSuiteLabel)

		} else if openshiftJobSpec.Refs.Repo == "infra-deployments" {
			os.Setenv("INFRA_DEPLOYMENTS_ORG", pr.RemoteName)
			os.Setenv("INFRA_DEPLOYMENTS_BRANCH", pr.BranchName)
			os.Setenv("E2E_TEST_SUITE_LABEL", "e2e-demo,mvp-demo,spi-suite,integration-service,o11y")
		}
	} else {
		if ci.isPRPairingRequired("infra-deployments") {
			os.Setenv("INFRA_DEPLOYMENTS_ORG", pr.RemoteName)
			os.Setenv("INFRA_DEPLOYMENTS_BRANCH", pr.BranchName)
		}
	}

	return nil
}

func BootstrapCluster() error {
	envVars := map[string]string{}

	if os.Getenv("CI") == "true" && os.Getenv("REPO_NAME") == "e2e-tests" {
		// Some scripts in infra-deployments repo are referencing scripts/utils in e2e-tests repo
		// This env var allows to test changes introduced in "e2e-tests" repo PRs in CI
		envVars["E2E_TESTS_COMMIT_SHA"] = os.Getenv("PULL_PULL_SHA")
	}

	ic, err := installation.NewAppStudioInstallController()
	if err != nil {
		return fmt.Errorf("failed to initialize installation controller: %+v", err)
	}

	return ic.InstallAppStudioPreviewMode()
}

func (CI) isPRPairingRequired(repoForPairing string) bool {
	var pullRequests []gh.PullRequest

	url := fmt.Sprintf("https://api.github.com/repos/redhat-appstudio/%s/pulls?per_page=100", repoForPairing)
	if err := sendHttpRequestAndParseResponse(url, "GET", &pullRequests); err != nil {
		klog.Infof("cannot determine %s Github branches for author %s: %v. will stick with the redhat-appstudio/%s main branch for running tests", repoForPairing, pr.RemoteName, err, repoForPairing)
		return false
	}

	for _, pull := range pullRequests {
		if pull.GetHead().GetRef() == pr.BranchName && pull.GetUser().GetLogin() == pr.RemoteName {
			return true
		}
	}

	return false
}

func (CI) sendWebhook() error {
	// AppStudio QE webhook configuration values will be used by default (if none are provided via env vars)
	const appstudioQESaltSecret = "123456789"
	const appstudioQEWebhookTargetURL = "https://hook.pipelinesascode.com/EyFYTakxEgEy"

	var repoURL string

	var repoOwner = os.Getenv("REPO_OWNER")
	var repoName = os.Getenv("REPO_NAME")
	var prNumber = os.Getenv("PULL_NUMBER")
	var saltSecret = utils.GetEnv("WEBHOOK_SALT_SECRET", appstudioQESaltSecret)
	var webhookTargetURL = utils.GetEnv("WEBHOOK_TARGET_URL", appstudioQEWebhookTargetURL)

	if strings.Contains(jobName, "hacbs-e2e-periodic") {
		// TODO configure webhook channel for sending HACBS test results
		klog.Infof("not sending webhook for HACBS periodic job yet")
		return nil
	}

	if jobType == "periodic" {
		repoURL = "https://github.com/redhat-appstudio/infra-deployments"
		repoOwner = "redhat-appstudio"
		repoName = "infra-deployments"
		prNumber = "periodic"
	} else if repoName == "e2e-tests" || repoName == "infra-deployments" {
		repoURL = openshiftJobSpec.Refs.RepoLink
	} else {
		klog.Infof("sending webhook for jobType %s, jobName %s is not supported", jobType, jobName)
		return nil
	}

	path, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error when sending webhook: %+v", err)
	}

	wh := Webhook{
		Path: path,
		Repository: Repository{
			FullName:   fmt.Sprintf("%s/%s", repoOwner, repoName),
			PullNumber: prNumber,
		},
		RepositoryURL: repoURL,
	}
	resp, err := wh.CreateAndSend(saltSecret, webhookTargetURL)
	if err != nil {
		return fmt.Errorf("error sending webhook: %+v", err)
	}
	klog.Infof("webhook response: %+v", resp)

	return nil
}

// Generates test cases for Polarion(polarion.xml) from test files for AppStudio project.
func GenerateTestCasesAppStudio() error {
	return sh.RunV("ginkgo", "--dry-run", "--label-filter=$E2E_TEST_SUITE_LABEL", "./cmd", "--", "--polarion-output-file=polarion.xml", "--generate-test-cases=true")
}

// I've attached to the Local struct for now since it felt like it fit but it can be decoupled later as a standalone func.
func (Local) GenerateTestSuiteFile() error {

	var templatePackageName = utils.GetEnv("TEMPLATE_SUITE_PACKAGE_NAME", "")
	var templatePath = "templates/test_suite_cmd.tmpl"
	var err error

	if !utils.CheckIfEnvironmentExists("TEMPLATE_SUITE_PACKAGE_NAME") {
		klog.Exitf("TEMPLATE_SUITE_PACKAGE_NAME not set or is empty")
	}

	var templatePackageFile = fmt.Sprintf("cmd/%s_test.go", templatePackageName)
	klog.Infof("Creating new test suite file %s.\n", templatePackageFile)
	data := TemplateData{SuiteName: templatePackageName}
	err = renderTemplate(templatePackageFile, templatePath, data, false)

	if err != nil {
		klog.Errorf("failed to render template with: %s", err)
		return err
	}

	err = goFmt(templatePackageFile)
	if err != nil {

		klog.Errorf("%s", err)
		return err
	}

	return nil
}

// Remove all webhooks which with 1 day lifetime. By default will delete webooks from redhat-appstudio-qe
func CleanWebHooks() error {
	token := utils.GetEnv(constants.GITHUB_TOKEN_ENV, "")
	if token == "" {
		return fmt.Errorf("empty GITHUB_TOKEN env. Please provide a valid github token")
	}

	githubOrg := utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")
	gh, err := github.NewGithubClient(token, githubOrg)
	if err != nil {
		return err
	}
	for _, repo := range repositoriesWithWebhooks {
		webhookList, err := gh.ListRepoWebhooks(repo)
		if err != nil {
			return err
		}
		for _, wh := range webhookList {
			dayDuration, _ := time.ParseDuration("24h")
			if time.Since(wh.GetCreatedAt()) > dayDuration {
				klog.Infof("removing webhook: %s, git_organization: %s, git_repository: %s", wh.GetName(), githubOrg, repo)
				if err := gh.DeleteWebhook(repo, wh.GetID()); err != nil {
					return fmt.Errorf("failed to delete webhook: %v, repo: %s", wh.Name, repo)
				}
			}
		}
	}
	return nil
}

// I've attached to the Local struct for now since it felt like it fit but it can be decoupled later as a standalone func.
func (Local) GenerateTestSpecFile() error {

	var templateDirName = utils.GetEnv("TEMPLATE_SUITE_PACKAGE_NAME", "")
	var templateSpecName = utils.GetEnv("TEMPLATE_SPEC_FILE_NAME", templateDirName)
	var templateAppendFrameworkDescribeBool = utils.GetEnv("TEMPLATE_APPEND_FRAMEWORK_DESCRIBE_FILE", "true")
	var templateJoinSuiteSpecNamesBool = utils.GetEnv("TEMPLATE_JOIN_SUITE_SPEC_FILE_NAMES", "false")
	var templatePath = "templates/test_spec_file.tmpl"
	var templateDirPath string
	var templateSpecFile string
	var err error
	var caser = cases.Title(language.English)

	if !utils.CheckIfEnvironmentExists("TEMPLATE_SUITE_PACKAGE_NAME") {
		klog.Exitf("TEMPLATE_SUITE_PACKAGE_NAME not set or is empty")
	}

	if !utils.CheckIfEnvironmentExists("TEMPLATE_SPEC_FILE_NAME") {
		klog.Infof("TEMPLATE_SPEC_FILE_NAME not set. Defaulting test spec file to value of `%s`.\n", templateSpecName)
	}

	if !utils.CheckIfEnvironmentExists("TEMPLATE_APPEND_FRAMEWORK_DESCRIBE_FILE") {
		klog.Infof("TEMPLATE_APPEND_FRAMEWORK_DESCRIBE_FILE not set. Defaulting to `%s` which will update the pkg/framework/describe.go after generating the template.\n", templateAppendFrameworkDescribeBool)
	}

	if utils.CheckIfEnvironmentExists("TEMPLATE_JOIN_SUITE_SPEC_FILE_NAMES") {
		klog.Infof("TEMPLATE_JOIN_SUITE_SPEC_FILE_NAMES is set to %s. Will join the suite package and spec file name to be used in the Describe of suites.\n", templateJoinSuiteSpecNamesBool)
	}

	templateDirPath = fmt.Sprintf("tests/%s", templateDirName)
	err = os.MkdirAll(templateDirPath, 0775)
	if err != nil {
		klog.Errorf("failed to create package directory, %s, template with: %v", templateDirPath, err)
		return err
	}
	templateSpecFile = fmt.Sprintf("%s/%s.go", templateDirPath, templateSpecName)

	klog.Infof("Creating new test package directory and spec file %s.\n", templateSpecFile)
	if templateJoinSuiteSpecNamesBool == "true" {
		templateSpecName = fmt.Sprintf("%s%v", caser.String(templateDirName), caser.String(templateSpecName))
	} else {
		templateSpecName = caser.String(templateSpecName)
	}

	data := TemplateData{SuiteName: templateDirName,
		TestSpecName: templateSpecName}
	err = renderTemplate(templateSpecFile, templatePath, data, false)
	if err != nil {
		klog.Errorf("failed to render template with: %s", err)
		return err
	}

	err = goFmt(templateSpecFile)
	if err != nil {

		klog.Errorf("%s", err)
		return err
	}

	if templateAppendFrameworkDescribeBool == "true" {

		err = appendFrameworkDescribeFile(templateSpecName)

		if err != nil {
			return err
		}

	}

	return nil
}

func appendFrameworkDescribeFile(packageName string) error {

	var templatePath = "templates/framework_describe_func.tmpl"
	var describeFile = "pkg/framework/describe.go"
	var err error
	var caser = cases.Title(language.English)

	data := TemplateData{TestSpecName: caser.String(packageName)}
	err = renderTemplate(describeFile, templatePath, data, true)
	if err != nil {
		klog.Errorf("failed to append to pkg/framework/describe.go with : %s", err)
		return err
	}
	err = goFmt(describeFile)

	if err != nil {

		klog.Errorf("%s", err)
		return err
	}

	return nil

}
