package framework

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/avast/retry-go/v4"
	"github.com/konflux-ci/e2e-tests/pkg/clients/common"
	"github.com/konflux-ci/e2e-tests/pkg/clients/gitops"
	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	"github.com/konflux-ci/e2e-tests/pkg/clients/imagecontroller"
	"github.com/konflux-ci/e2e-tests/pkg/clients/integration"
	"github.com/konflux-ci/e2e-tests/pkg/clients/jvmbuildservice"
	kubeCl "github.com/konflux-ci/e2e-tests/pkg/clients/kubernetes"
	"github.com/konflux-ci/e2e-tests/pkg/clients/release"
	"github.com/konflux-ci/e2e-tests/pkg/clients/tekton"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/sandbox"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
)

type ControllerHub struct {
	HasController             *has.HasController
	CommonController          *common.SuiteController
	TektonController          *tekton.TektonController
	GitOpsController          *gitops.GitopsController
	ReleaseController         *release.ReleaseController
	IntegrationController     *integration.IntegrationController
	JvmbuildserviceController *jvmbuildservice.JvmbuildserviceController
	ImageController           *imagecontroller.ImageController
}

type Framework struct {
	AsKubeAdmin          *ControllerHub
	AsKubeDeveloper      *ControllerHub
	ClusterAppDomain     string
	OpenshiftConsoleHost string
	ProxyUrl             string
	SandboxController    *sandbox.SandboxController
	UserNamespace        string
	UserName             string
	UserToken            string
}

func NewFramework(userName string, stageConfig ...utils.Options) (*Framework, error) {
	return NewFrameworkWithTimeout(userName, time.Second*60, stageConfig...)
}

// This periodically refreshes framework for Stage user because of Keycloak access token expires in 15 minutes
func refreshFrameworkStage(currentFramework *Framework, userName string, timeout time.Duration, options ...utils.Options) {
	for {
		time.Sleep(time.Minute * 10)
		fw, err := newFrameworkWithTimeout(userName, timeout, options...)
		if err != nil {
			fmt.Printf("ERROR: Failed refreshing framework for user %s: %+v\n", userName, err)
			return
		}
		*currentFramework = *fw
	}
}

func newFrameworkWithTimeout(userName string, timeout time.Duration, options ...utils.Options) (*Framework, error) {
	var err error
	var k *kubeCl.K8SClient
	var clusterAppDomain, openshiftConsoleHost string
	var option utils.Options

	if userName == "" {
		return nil, fmt.Errorf("userName cannot be empty when initializing a new framework instance")
	}
	isStage, err := utils.CheckOptions(options)
	if err != nil {
		return nil, err
	}
	if len(options) == 1 {
		option = options[0]
	} else {
		option = utils.Options{}
	}
	// https://issues.redhat.com/browse/CRT-1670
	if len(userName) > 20 {
		GinkgoWriter.Printf("WARNING: username %q is longer than 20 characters - the tenant namespace prefix will be shortened to %s\n", userName, userName[:20])
	}

	// in some very rare cases fail to get the client for some timeout in member operator.
	// Just try several times to get the user kubeconfig

	err = retry.Do(
		func() error {
			if k, err = kubeCl.NewDevSandboxProxyClient(userName, isStage, option); err != nil {
				GinkgoWriter.Printf("error when creating dev sandbox proxy client: %+v\n", err)
			}
			return err
		},
		retry.Attempts(20),
	)

	if err != nil {
		return nil, fmt.Errorf("error when initializing kubernetes clients: %v", err)
	}

	var asAdmin *ControllerHub
	if !isStage {
		asAdmin, err = InitControllerHub(k.AsKubeAdmin)
		if err != nil {
			return nil, fmt.Errorf("error when initializing appstudio hub controllers for admin user: %v", err)
		}
		if err = asAdmin.CommonController.AddRegistryAuthSecretToSA("QUAY_TOKEN", k.UserNamespace); err != nil {
			GinkgoWriter.Println(fmt.Sprintf("Failed to add registry auth secret to service account: %v\n", err))
		}
	}

	asUser, err := InitControllerHub(k.AsKubeDeveloper)
	if err != nil {
		return nil, fmt.Errorf("error when initializing appstudio hub controllers for sandbox user: %v", err)
	}

	if isStage {
		asAdmin = asUser
	}

	if !isStage {
		if err = utils.WaitUntil(asAdmin.CommonController.ServiceAccountPresent(constants.DefaultPipelineServiceAccount, k.UserNamespace), timeout); err != nil {
			return nil, fmt.Errorf("'%s' service account wasn't created in %s namespace: %+v", constants.DefaultPipelineServiceAccount, k.UserNamespace, err)
		}
		r, err := asAdmin.CommonController.CustomClient.RouteClient().RouteV1().Routes("openshift-console").Get(context.Background(), "console", v1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot get openshift console route in order to determine cluster app domain: %+v", err)
		}
		openshiftConsoleHost = r.Spec.Host
		clusterAppDomain = strings.Join(strings.Split(openshiftConsoleHost, ".")[1:], ".")
	}
	return &Framework{
		AsKubeAdmin:          asAdmin,
		AsKubeDeveloper:      asUser,
		ClusterAppDomain:     clusterAppDomain,
		OpenshiftConsoleHost: openshiftConsoleHost,
		ProxyUrl:             k.ProxyUrl,
		SandboxController:    k.SandboxController,
		UserNamespace:        k.UserNamespace,
		UserName:             k.UserName,
		UserToken:            k.UserToken,
	}, nil
}

func NewFrameworkWithTimeout(userName string, timeout time.Duration, options ...utils.Options) (*Framework, error) {
	isStage, err := utils.CheckOptions(options)
	if err != nil {
		return nil, err
	}

	if isStage {
		options[0].ToolchainApiUrl = fmt.Sprintf("%s/workspaces/%s", options[0].ToolchainApiUrl, userName)
	}

	fw, err := newFrameworkWithTimeout(userName, timeout, options...)

	if isStage {
		go refreshFrameworkStage(fw, userName, timeout, options...)
	}

	return fw, err
}

func InitControllerHub(cc *kubeCl.CustomClient) (*ControllerHub, error) {
	// Initialize Common controller
	commonCtrl, err := common.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Has controller
	hasController, err := has.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Tekton controller
	tektonController := tekton.NewSuiteController(cc)

	// Initialize GitOps controller
	gitopsController, err := gitops.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Release Controller
	releaseController, err := release.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Integration Controller
	integrationController, err := integration.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize JVM Build Service Controller
	jvmbuildserviceController, err := jvmbuildservice.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Image Controller
	imageController, err := imagecontroller.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	return &ControllerHub{
		HasController:             hasController,
		CommonController:          commonCtrl,
		TektonController:          tektonController,
		GitOpsController:          gitopsController,
		ReleaseController:         releaseController,
		IntegrationController:     integrationController,
		JvmbuildserviceController: jvmbuildserviceController,
		ImageController:           imageController,
	}, nil
}
