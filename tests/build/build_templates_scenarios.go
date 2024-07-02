package build

import (
	"github.com/konflux-ci/e2e-tests/pkg/constants"
)

type ComponentScenarioSpec struct {
	GitURL              string
	ContextDir          string
	DockerFilePath      string
	PipelineBundleName  string
	EnableHermetic      bool
	PrefetchInput       string
	CheckAdditionalTags bool
}

var componentScenarios = []ComponentScenarioSpec{
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/devfile-sample-python-basic",
		ContextDir:         ".",
		DockerFilePath:     constants.DockerFilePath,
		PipelineBundleName: "docker-build",
		EnableHermetic:     false,
		PrefetchInput:      "",
	},
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/retrodep",
		ContextDir:         ".",
		DockerFilePath:     "Dockerfile",
		PipelineBundleName: "docker-build",
		EnableHermetic:     true,
		PrefetchInput:      "gomod",
	},
	{
		GitURL:              "https://github.com/cachito-testing/pip-e2e-test",
		ContextDir:          ".",
		DockerFilePath:      "Dockerfile",
		PipelineBundleName:  "docker-build",
		EnableHermetic:      true,
		PrefetchInput:       "pip",
		CheckAdditionalTags: true,
	},
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/fbc-sample-repo",
		ContextDir:         "4.13",
		DockerFilePath:     "catalog.Dockerfile",
		PipelineBundleName: "fbc-builder",
		EnableHermetic:     false,
		PrefetchInput:      "",
	},
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/source-build-parent-image-with-digest-only",
		ContextDir:         ".",
		DockerFilePath:     "Dockerfile",
		PipelineBundleName: "docker-build",
		EnableHermetic:     false,
		PrefetchInput:      "",
	},
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/source-build-use-latest-parent-image",
		ContextDir:         ".",
		DockerFilePath:     "Dockerfile",
		PipelineBundleName: "docker-build",
		EnableHermetic:     false,
		PrefetchInput:      "",
	},
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/source-build-parent-image-from-registry-rh-io",
		ContextDir:         ".",
		DockerFilePath:     "Dockerfile",
		PipelineBundleName: "docker-build",
		EnableHermetic:     false,
		PrefetchInput:      "",
	},
	{
		GitURL:             "https://github.com/redhat-appstudio-qe/source-build-base-on-konflux-image",
		ContextDir:         ".",
		DockerFilePath:     "Dockerfile",
		PipelineBundleName: "docker-build",
		EnableHermetic:     false,
		PrefetchInput:      "",
	},
}

func GetComponentScenarioDetailsFromGitUrl(gitUrl string) (string, string, string, bool, string, bool) {
	for _, componentScenario := range componentScenarios {
		if componentScenario.GitURL == gitUrl {
			return componentScenario.ContextDir, componentScenario.DockerFilePath, componentScenario.PipelineBundleName, componentScenario.EnableHermetic, componentScenario.PrefetchInput, componentScenario.CheckAdditionalTags
		}
	}
	return "", "", "", false, "", false
}
