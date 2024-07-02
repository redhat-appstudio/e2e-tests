package common

import (
	"time"

	"github.com/konflux-ci/e2e-tests/pkg/utils"
	corev1 "k8s.io/api/core/v1"
)

const (
	ApplicationNameDefault       string = "appstudio"
	ReleaseStrategyPolicyDefault string = "mvp-policy"
	ReleaseStrategyPolicy        string = "policy"

	RedhatAppstudioUserSecret            string = "hacbs-release-tests-token"
	RedhatAppstudioQESecret              string = "redhat-appstudio-qe-bot-token"
	HacbsReleaseTestsTokenSecret         string = "redhat-appstudio-registry-pull-secret"
	PublicSecretNameAuth                 string = "cosign-public-key"
	ReleasePipelineServiceAccountDefault string = "release-service-account"

	SourceReleasePlanName          string = "source-releaseplan"
	SecondReleasePlanName          string = "the-second-releaseplan"
	TargetReleasePlanAdmissionName string = "demo"
	ReleasePvcName                 string = "release-pvc"
	ReleaseEnvironment             string = "production"

	ReleaseCreationTimeout              = 5 * time.Minute
	ReleasePipelineRunCreationTimeout   = 10 * time.Minute
	ReleasePipelineRunCompletionTimeout = 60 * time.Minute
	BuildPipelineRunCompletionTimeout   = 60 * time.Minute
	BuildPipelineRunCreationTimeout     = 10 * time.Minute
	ReleasePlanStatusUpdateTimeout      = 1 * time.Minute
	DefaultInterval                     = 100 * time.Millisecond

	// Pipelines constants
	ComponentName                   string = "dc-metro-map"
	GitSourceComponentUrl           string = "https://github.com/scoheb/dc-metro-map"
	AdditionalComponentName         string = "simple-python"
	MultiArchComponentUrl           string = "https://github.com/jinqi7/multi-platform-test-prod"
	AdditionalGitSourceComponentUrl string = "https://github.com/devfile-samples/devfile-sample-python-basic"
	ReleasedImagePushRepo           string = "quay.io/redhat-appstudio-qe/dcmetromap"
	AdditionalReleasedImagePushRepo string = "quay.io/redhat-appstudio-qe/simplepython"
	PyxisStageImagesApiEndpoint     string = "https://pyxis.preprod.api.redhat.com/v1/images/id/"
	GitLabRunFileUpdatesTestRepo    string = "https://gitlab.cee.redhat.com/hacbs-release-tests/app-interface"

	// EC constants
	EcPolicyLibPath     = "github.com/enterprise-contract/ec-policies//policy/lib"
	EcPolicyReleasePath = "github.com/enterprise-contract/ec-policies//policy/release"
	EcPolicyDataBundle  = "oci::quay.io/redhat-appstudio-tekton-catalog/data-acceptable-bundles:latest"
	EcPolicyDataPath    = "github.com/release-engineering/rhtap-ec-policy//data"

	// Service constants
	ApplicationName string = "application"
)

var ManagednamespaceSecret = []corev1.ObjectReference{
	{Name: RedhatAppstudioUserSecret},
}

// Pipelines variables
var (
	RelSvcCatalogURL      string = utils.GetEnv("RELEASE_SERVICE_CATALOG_URL", "https://github.com/konflux-ci/release-service-catalog")
	RelSvcCatalogRevision string = utils.GetEnv("RELEASE_SERVICE_CATALOG_REVISION", "staging")
)
