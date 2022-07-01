package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/magefile/mage/sh"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"k8s.io/klog/v2"
)

var (
	requiredBinaries = []string{"jq", "kubectl", "oc", "yq", "jq", "git"}
	artifactDir      = utils.GetEnv("ARTIFACT_DIR", ".")
	openshiftJobSpec = &OpenshiftJobSpec{}
	pr               = &PullRequestMetadata{}
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

	if err = ci.parseJobSpec(); err != nil {
		return err
	}

	pr.Author = openshiftJobSpec.Refs.Pulls[0].Author
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

	if err := ci.init(); err != nil {
		return err
	}

	if openshiftJobSpec.Refs.Repo == "e2e-tests" {
		gitCheckoutRemoteBranch(pr.Author, pr.CommitSHA)
	} else {
		if ci.isPRPairingRequired() {
			gitCheckoutRemoteBranch(pr.Author, pr.BranchName)
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

	if err := ConfigureSPI(); err != nil {
		return fmt.Errorf("error when configuring SPI: %v", err)
	}

	if err := RunAppstudioHealthChecks(); err != nil {
		return fmt.Errorf("error when running appstudio healthchecks: %v", err)
	}
	return nil
}

func (Local) TestE2E() error {
	return RunE2ETests()
}

func (ci CI) TestE2E() error {

	if err := ci.init(); err != nil {
		return err
	}

	if err := PreflightChecks(); err != nil {
		return fmt.Errorf("error when running preflight checks: %v", err)
	}

	if err := ci.setRequiredEnvVars(); err != nil {
		return fmt.Errorf("error when setting up required env vars: %v", err)
	}

	if err := ci.createOpenshiftUser(); err != nil {
		return fmt.Errorf("error when creating openshift user: %v", err)
	}

	if err := BootstrapCluster(); err != nil {
		return fmt.Errorf("error when bootstrapping cluster: %v", err)
	}

	if err := ConfigureSPI(); err != nil {
		return fmt.Errorf("error when configuring SPI: %v", err)
	}

	if err := RunAppstudioHealthChecks(); err != nil {
		return fmt.Errorf("error when running appstudio healthchecks: %v", err)
	}

	if err := RunE2ETests(); err != nil {
		return fmt.Errorf("error when running e2e tests: %v", err)
	}

	return nil
}

func RunE2ETests() error {
	cwd, _ := os.Getwd()

	// TODO add "-p" flag to run tests in parallel once our tests are prepared for it
	return sh.RunV("ginkgo", fmt.Sprintf("--output-dir=%s", artifactDir), "--junit-report=e2e-report.xml", "--v", "--progress", "--focus=$E2E_TEST_SUITE", "./cmd", "--", fmt.Sprintf("--config-suites=%s/tests/e2e-demos/config/default.yaml", cwd))
}

func PreflightChecks() error {
	if os.Getenv("GITHUB_TOKEN") == "" || os.Getenv("QUAY_TOKEN") == "" {
		return fmt.Errorf("required env vars containing secrets (QUAY_TOKEN, GITHUB_TOKEN) not defined or empty")
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

func (CI) setRequiredEnvVars() error {

	if openshiftJobSpec.Refs.Repo != "e2e-tests" {

		if strings.Contains(openshiftJobSpec.Refs.Repo, "-service") {
			var envVarPrefix, imageTagSuffix, testSuiteName string
			sp := strings.Split(os.Getenv("COMPONENT_IMAGE"), "@")

			switch openshiftJobSpec.Refs.Repo {
			case "application-service":
				envVarPrefix = "HAS"
				imageTagSuffix = "has-image"
				testSuiteName = "has-suite"
			case "build-service":
				envVarPrefix = "BUILD_SERVICE"
				imageTagSuffix = "build-service-image"
				testSuiteName = "build-service-suite"
			case "jvm-build-service":
				envVarPrefix = "JVM_BUILD_SERVICE"
				imageTagSuffix = "jvm-build-service-image"
				testSuiteName = "jvm-build-service-suite"
			}

			os.Setenv(fmt.Sprintf("%s_IMAGE_REPO", envVarPrefix), sp[0])
			os.Setenv(fmt.Sprintf("%s_IMAGE_TAG", envVarPrefix), fmt.Sprintf("redhat-appstudio-%s", imageTagSuffix))
			os.Setenv(fmt.Sprintf("%s_PR_OWNER", envVarPrefix), openshiftJobSpec.Refs.Pulls[0].Author)
			os.Setenv(fmt.Sprintf("%s_PR_SHA", envVarPrefix), openshiftJobSpec.Refs.Pulls[0].SHA)
			os.Setenv("E2E_TEST_SUITE", testSuiteName)

		} else if openshiftJobSpec.Refs.Repo == "infra-deployments" {

			os.Setenv("INFRA_DEPLOYMENTS_ORG", pr.Organization)
			os.Setenv("INFRA_DEPLOYMENTS_BRANCH", pr.BranchName)
		}

	}

	return nil
}

func (CI) createOpenshiftUser() error {
	tempKubeconfigPath := "/tmp/kubeconfig"
	os.Setenv("KUBECONFIG_TEST", tempKubeconfigPath)
	if err := sh.Run("./scripts/provision-openshift-user.sh"); err != nil {
		return err
	}
	os.Setenv("KUBECONFIG", tempKubeconfigPath)

	return nil
}

func BootstrapCluster() error {
	return sh.Run("./scripts/install-appstudio-e2e-mode.sh")
}

func ConfigureSPI() error {
	return sh.Run("./scripts/spi-e2e-setup.sh")
}

func RunAppstudioHealthChecks() error {
	return sh.Run("./scripts/run-appstudio-healthchecks.sh")
}

func (CI) isPRPairingRequired() bool {
	ghBranches := &GithubBranches{}
	if err := sendHttpRequestAndParseResponse(fmt.Sprintf("https://api.github.com/repos/%s/e2e-tests/branches", pr.Author), "GET", ghBranches); err != nil {
		klog.Infof("cannot determine e2e-tests Github branches for author %s: %v. will stick with the redhat-appstudio/e2e-tests main branch for running testss", pr.Author, err)
		return false
	}

	for _, b := range ghBranches.Branches {
		if b.Name == pr.BranchName {
			return true
		}
	}

	return false
}
