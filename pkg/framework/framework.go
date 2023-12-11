package framework

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"

	"github.com/avast/retry-go/v4"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/common"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/gitops"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/has"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/imagecontroller"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/integration"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/jvmbuildservice"
	kubeCl "github.com/redhat-appstudio/e2e-tests/pkg/clients/kubernetes"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/release"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/remotesecret"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/spi"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/tekton"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/sandbox"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"k8s.io/klog/v2"
)

type ControllerHub struct {
	HasController             *has.HasController
	CommonController          *common.SuiteController
	TektonController          *tekton.TektonController
	GitOpsController          *gitops.GitopsController
	SPIController             *spi.SPIController
	RemoteSecretController    *remotesecret.RemoteSecretController
	ReleaseController         *release.ReleaseController
	IntegrationController     *integration.IntegrationController
	JvmbuildserviceController *jvmbuildservice.JvmbuildserviceController
	ImageController           *imagecontroller.ImageController
}

type Framework struct {
	AsKubeAdmin       *ControllerHub
	AsKubeDeveloper   *ControllerHub
	ProxyUrl          string
	SandboxController *sandbox.SandboxController
	UserNamespace     string
	UserName          string
	UserToken         string
}

func NewFramework(userName string, stageConfig ...utils.Options) (*Framework, error) {
	return NewFrameworkWithTimeout(userName, time.Second*60, stageConfig...)
}

func NewFrameworkWithTimeout(userName string, timeout time.Duration, options ...utils.Options) (*Framework, error) {
	var err error
	var k *kubeCl.K8SClient
	var supplyopts utils.Options

	isStage, err := utils.CheckOptions(options)
	if err != nil {
		return nil, err
	}

	if isStage {
		// Debug
		klog.Infof("** options[0].ToolchainApiUrl before - %s; ", options[0].ToolchainApiUrl)

		options[0].ToolchainApiUrl = fmt.Sprintf("%s/workspaces/%s", options[0].ToolchainApiUrl, userName)

		// Debug
		klog.Infof("** options[0].ToolchainApiUrl after - %s; ", options[0].ToolchainApiUrl)

		supplyopts = options[0]
	}
	// https://issues.redhat.com/browse/CRT-1670
	if len(userName) > 20 {
		GinkgoWriter.Printf("WARNING: username %q is longer than 20 characters - the tenant namespace prefix will be shortened to %s\n", userName, userName[:20])
	}

	// in some very rare cases fail to get the client for some timeout in member operator.
	// Just try several times to get the user kubeconfig

	err = retry.Do(
		func() error {
			if k, err = kubeCl.NewDevSandboxProxyClient(userName, isStage, supplyopts); err != nil {
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
	}
	return &Framework{
		AsKubeAdmin:       asAdmin,
		AsKubeDeveloper:   asUser,
		ProxyUrl:          k.ProxyUrl,
		SandboxController: k.SandboxController,
		UserNamespace:     k.UserNamespace,
		UserName:          k.UserName,
		UserToken:         k.UserToken,
	}, nil
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

	// Initialize SPI controller
	spiController, err := spi.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Remote Secret controller
	remoteSecretController, err := remotesecret.NewSuiteController(cc)
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
		SPIController:             spiController,
		RemoteSecretController:    remoteSecretController,
		TektonController:          tektonController,
		GitOpsController:          gitopsController,
		ReleaseController:         releaseController,
		IntegrationController:     integrationController,
		JvmbuildserviceController: jvmbuildserviceController,
		ImageController:           imageController,
	}, nil
}
