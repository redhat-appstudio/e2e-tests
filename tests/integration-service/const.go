package integration

import (
	"fmt"

	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
)

const (
	componentRepoURL = "https://github.com/redhat-appstudio-qe/hacbs-test-project"

	EnvironmentName                = "development"
	gitURL                         = "https://github.com/konflux-ci/integration-examples.git"
	revision                       = "ab868616ab02be79b6abdf85dcd2a3aef321ff14"
	pathInRepoPass                 = "pipelines/integration_resolver_pipeline_pass.yaml"
	pathIntegrationPipelineWithEnv = "pipelines/integration_resolver_pipeline_environment_pass.yaml"
	pathInRepoFail                 = "pipelines/integration_resolver_pipeline_fail.yaml"
	autoReleasePlan                = "auto-releaseplan"
	targetReleaseNamespace         = "default"

	componentRepoNameForStatusReporting       = "hacbs-test-project-integration"
	componentDefaultBranch                    = "main"
	componentRevision                         = "34da5a8f51fba6a8b7ec75a727d3c72ebb5e1274"
	referenceDoesntExist                      = "Reference does not exist"
	checkrunStatusCompleted                   = "completed"
	checkrunConclusionSuccess                 = "success"
	checkrunConclusionFailure                 = "failure"
	integrationPipelineRunCommitStatusSuccess = "success"
	integrationPipelineRunCommitStatusFail    = "failed"
	spaceRequestCronJobNamespace              = "spacerequest-cleaner"
	spaceRequestCronJobName                   = "spacerequest-cleaner"
	spaceRequestNamePrefix                    = "task-spacerequest-"

	snapshotAnnotation                       = "appstudio.openshift.io/snapshot"
	scenarioAnnotation                       = "test.appstudio.openshift.io/scenario"
	pipelinerunFinalizerByIntegrationService = "test.appstudio.openshift.io/pipelinerun"
	snapshotRerunLabel                       = "test.appstudio.openshift.io/run"

	chainsSignedAnnotation = "chains.tekton.dev/signed"
)

var (
	componentGitSourceURLForStatusReporting       = fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"), componentRepoNameForStatusReporting)
	gitlabComponentGitSourceURLForStatusReporting = fmt.Sprintf("https://gitlab.com/konflux-qe/%s", componentRepoNameForStatusReporting)
)
