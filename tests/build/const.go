package build

import (
	"fmt"
	"strings"

	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
)

const (
	COMPONENT_REPO_URLS_ENV string = "COMPONENT_REPO_URLS"

	containerImageSource             = "quay.io/redhat-appstudio-qe/busybox-loop@sha256:f698f1f2cf641fe9176d2a277c9052d872f6b1c39e56248a1dd259b96281dda9"
	pythonComponentGitSourceURL      = "https://github.com/redhat-appstudio-qe/devfile-sample-python-basic"
	gitRepoContainsSymlinkBranchName = "symlink"
	dummyPipelineBundleRef           = "quay.io/redhat-appstudio-qe/dummy-pipeline-bundle@sha256:9805fc3f309af8f838622e49d3e7705d8364eb5c8287043d5725f3ef12232f24"
	buildTemplatesTestLabel          = "build-templates-e2e"
	buildTemplatesKcpTestLabel       = "build-templates-kcp-e2e"
	sourceBuildTestLabel             = "source-build-e2e"

	helloWorldComponentGitSourceRepoName = "devfile-sample-hello-world"
	helloWorldComponentDefaultBranch     = "default"
	helloWorldComponentRevision          = "d2d03e69de912e3827c29b4c5b71ffe8bcb5dad8"

	multiComponentGitSourceRepoName = "sample-multi-component"
	multiComponentDefaultBranch     = "main"
	multiComponentGitRevision       = "0d1835404efb8ab7bb1ab5b5b82cda1ebfda4b25"

	secretLookupGitSourceRepoOneName = "secret-lookup-sample-repo-one"
	secretLookupDefaultBranchOne     = "main"
	secretLookupGitRevisionOne       = "4b86bbfba19586f9ec8b648b3f47de3a5c62d460"
	secretLookupGitSourceRepoTwoName = "secret-lookup-sample-repo-two"
	secretLookupDefaultBranchTwo     = "main"
	secretLookupGitRevisionTwo       = "9fd1358a22212d03ed938ea3bed8df98dddd2652"

	annotationsTestGitSourceRepoName = "multi-stage-build-go-sample"
	annotationsTestRevision          = "529f65798777a5fe145e33d58e1e91c4c03704a4"

	componentDependenciesParentRepoName      = "build-nudge-parent"
	componentDependenciesParentDefaultBranch = "main"
	componentDependenciesParentGitRevision   = "a8dce08dbdf290e5d616a83672ad3afcb4b455ef"
	componentDependenciesChildRepoName       = "build-nudge-child"
	componentDependenciesChildDefaultBranch  = "main"
	componentDependenciesChildGitRevision    = "56c13d17b1a8f801f2c41091e6f4e62cf16ee5f2"

	githubUrlFormat = "https://github.com/%s/%s"

	appstudioCrdsBuild = "appstudio-crds-build"
	computeBuild       = "compute-build"

	//Logging related
	buildStatusAnnotationValueLoggingFormat = "build status annotation value: %s\n"

	noAppOrgName = "redhat-appstudio-qe-no-app"
)

var (
	additionalTags                  = []string{"test-tag1", "test-tag2"}
	componentUrls                   = strings.Split(utils.GetEnv(COMPONENT_REPO_URLS_ENV, pythonComponentGitSourceURL), ",") //multiple urls
	componentNames                  []string
	gihubOrg                        = utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")
	helloWorldComponentGitSourceURL = fmt.Sprintf(githubUrlFormat, gihubOrg, helloWorldComponentGitSourceRepoName)
	annotationsTestGitSourceURL     = fmt.Sprintf(githubUrlFormat, gihubOrg, annotationsTestGitSourceRepoName)
	multiComponentGitSourceURL      = fmt.Sprintf(githubUrlFormat, gihubOrg, multiComponentGitSourceRepoName)
	multiComponentContextDirs       = []string{"go-component", "python-component"}

	secretLookupComponentOneGitSourceURL = fmt.Sprintf(githubUrlFormat, noAppOrgName, secretLookupGitSourceRepoOneName)
	secretLookupComponentTwoGitSourceURL = fmt.Sprintf(githubUrlFormat, noAppOrgName, secretLookupGitSourceRepoTwoName)
)
