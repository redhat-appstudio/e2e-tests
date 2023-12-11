package client

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	ocpOauth "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	routeclientset "github.com/openshift/client-go/route/clientset/versioned"
	appstudioApi "github.com/redhat-appstudio/application-api/api/v1alpha1"
	buildservice "github.com/redhat-appstudio/build-service/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/sandbox"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	imagecontroller "github.com/redhat-appstudio/image-controller/api/v1alpha1"
	integrationservice "github.com/redhat-appstudio/integration-service/api/v1alpha1"
	integrationservicev1beta1 "github.com/redhat-appstudio/integration-service/api/v1beta1"
	jvmbuildservice "github.com/redhat-appstudio/jvm-build-service/pkg/apis/jvmbuildservice/v1alpha1"
	jvmbuildserviceclientset "github.com/redhat-appstudio/jvm-build-service/pkg/client/clientset/versioned"
	gitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/backend-shared/apis/managed-gitops/v1alpha1"
	release "github.com/redhat-appstudio/release-service/api/v1alpha1"
	rs "github.com/redhat-appstudio/remote-secret/api/v1beta1"
	spi "github.com/redhat-appstudio/service-provider-integration-operator/api/v1beta1"
	tekton "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	pipelineclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	DefaultRetryInterval = time.Millisecond * 100 // make it short because a "retry interval" is waited before the first test
	DefaultTimeout       = time.Second * 240
)

type CustomClient struct {
	kubeClient            *kubernetes.Clientset
	crClient              crclient.Client
	pipelineClient        pipelineclientset.Interface
	dynamicClient         dynamic.Interface
	jvmbuildserviceClient jvmbuildserviceclientset.Interface
	routeClient           routeclientset.Interface
}

type K8SClient struct {
	AsKubeAdmin       *CustomClient
	AsKubeDeveloper   *CustomClient
	ProxyUrl          string
	SandboxController *sandbox.SandboxController
	UserName          string
	UserNamespace     string
	UserToken         string
}

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appstudioApi.AddToScheme(scheme))
	utilruntime.Must(ocpOauth.AddToScheme(scheme))
	utilruntime.Must(tekton.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(spi.AddToScheme(scheme))
	utilruntime.Must(toolchainv1alpha1.AddToScheme(scheme))
	utilruntime.Must(release.AddToScheme(scheme))
	utilruntime.Must(gitopsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(integrationservice.AddToScheme(scheme))
	utilruntime.Must(integrationservicev1beta1.AddToScheme(scheme))
	utilruntime.Must(jvmbuildservice.AddToScheme(scheme))
	utilruntime.Must(ecp.AddToScheme(scheme))
	utilruntime.Must(buildservice.AddToScheme(scheme))
	utilruntime.Must(userv1.AddToScheme(scheme))
	utilruntime.Must(rs.AddToScheme(scheme))
	utilruntime.Must(imagecontroller.AddToScheme(scheme))
}

// Kube returns the clientset for Kubernetes upstream.
func (c *CustomClient) KubeInterface() kubernetes.Interface {
	return c.kubeClient
}

// Return a rest client to perform CRUD operations on Kubernetes objects
func (c *CustomClient) KubeRest() crclient.Client {
	return c.crClient
}

func (c *CustomClient) PipelineClient() pipelineclientset.Interface {
	return c.pipelineClient
}

func (c *CustomClient) JvmbuildserviceClient() jvmbuildserviceclientset.Interface {
	return c.jvmbuildserviceClient
}

func (c *CustomClient) RouteClient() routeclientset.Interface {
	return c.routeClient
}

// Returns a DynamicClient interface.
// Note: other client interfaces are likely preferred, except in rare cases.
func (c *CustomClient) DynamicClient() dynamic.Interface {
	return c.dynamicClient
}

// Creates Kubernetes clients:
// 1. Will create a kubernetes client from default kubeconfig as kubeadmin
// 2. Will create a sandbox user and will generate a client using user token a new client to create resources in RHTAP like a normal user
func NewDevSandboxProxyClient(userName string, isStage bool, options utils.Options) (*K8SClient, error) {
	var err error
	var asAdminClient *CustomClient = nil
	var sandboxController *sandbox.SandboxController
	var proxyAuthInfo *sandbox.SandboxUserAuthInfo
	var sandboxProxyClient *CustomClient

	if isStage {
		// Debug
		klog.Infof("** userName - %s; options.ToolchainApiUrl - %s; options.KeycloakUrl - %s; options.OfflineToken - %s ", userName, options.ToolchainApiUrl, options.KeycloakUrl, options.OfflineToken)

		sandboxController, err = sandbox.NewDevSandboxStageController()
		if err != nil {
			// Debug
			klog.Infof("** Failure in NewDevSandboxStageController")

			return nil, err
		}
		proxyAuthInfo, err = sandboxController.ReconcileUserCreationStage(userName, options.ToolchainApiUrl, options.KeycloakUrl, options.OfflineToken)
		if err != nil {
			// Debug
			klog.Infof("** Failure in ReconcileUserCreationStage")

			return nil, err
		}

	} else {
		asAdminClient, err = NewAdminKubernetesClient()
		if err != nil {
			return nil, err
		}

		sandboxController, err = sandbox.NewDevSandboxController(asAdminClient.KubeInterface(), asAdminClient.KubeRest())
		if err != nil {
			return nil, err
		}

		proxyAuthInfo, err = sandboxController.ReconcileUserCreation(userName)
		if err != nil {
			return nil, err
		}
	}

	sandboxProxyClient, err = CreateAPIProxyClient(proxyAuthInfo.UserToken, proxyAuthInfo.ProxyUrl)
	if err != nil {
		// Debug
		klog.Infof("** Failure in CreateAPIProxyClient")
		klog.Infof("** proxyAuthInfo.UserToken - %s; proxyAuthInfo.ProxyUrl - %s", proxyAuthInfo.UserToken, proxyAuthInfo.ProxyUrl)

		return nil, err
	}

	return &K8SClient{
		AsKubeAdmin:       asAdminClient,
		AsKubeDeveloper:   sandboxProxyClient,
		ProxyUrl:          proxyAuthInfo.ProxyUrl,
		SandboxController: sandboxController,
		UserName:          proxyAuthInfo.UserName,
		UserNamespace:     proxyAuthInfo.UserNamespace,
		UserToken:         proxyAuthInfo.UserToken,
	}, nil
}

// Creates a kubernetes client from default kubeconfig. Will take it from KUBECONFIG env if it is defined and if in case is not defined
// will create the client from $HOME/.kube/config
func NewAdminKubernetesClient() (*CustomClient, error) {
	adminKubeconfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	clientSets, err := createClientSetsFromConfig(adminKubeconfig)
	if err != nil {
		return nil, err
	}

	crClient, err := crclient.New(adminKubeconfig, crclient.Options{
		Scheme: scheme,
	})

	if err != nil {
		return nil, err
	}

	return &CustomClient{
		kubeClient:            clientSets.kubeClient,
		pipelineClient:        clientSets.pipelineClient,
		dynamicClient:         clientSets.dynamicClient,
		jvmbuildserviceClient: clientSets.jvmbuildserviceClient,
		routeClient:           clientSets.routeClient,
		crClient:              crClient,
	}, nil
}

// CreateAPIProxyClient creates a client to the RHTAP api proxy using the given user token
func CreateAPIProxyClient(usertoken, proxyURL string) (*CustomClient, error) {
	var proxyCl crclient.Client
	var initProxyClError error

	proxyKubeConfig := &rest.Config{
		Host:        proxyURL,
		BearerToken: usertoken,
		Transport:   noTimeoutDefaultTransport(),
	}

	// Getting the proxy client can fail from time to time if the proxy's informer cache has not been
	// updated yet and we try to create the client to quickly so retry to reduce flakiness.
	waitErr := wait.PollUntilContextTimeout(context.Background(), DefaultRetryInterval, DefaultTimeout, false, func(ctx context.Context) (done bool, err error) {
		proxyCl, initProxyClError = crclient.New(proxyKubeConfig, crclient.Options{Scheme: scheme})
		return initProxyClError == nil, nil
	})
	if waitErr != nil {
		return nil, initProxyClError
	}

	clientSets, err := createClientSetsFromConfig(proxyKubeConfig)
	if err != nil {
		return nil, err
	}

	return &CustomClient{
		kubeClient:            clientSets.kubeClient,
		pipelineClient:        clientSets.pipelineClient,
		dynamicClient:         clientSets.dynamicClient,
		jvmbuildserviceClient: clientSets.jvmbuildserviceClient,
		routeClient:           clientSets.routeClient,
		crClient:              proxyCl,
	}, nil
}

func noTimeoutDefaultTransport() *http.Transport {
	transport := http.DefaultTransport.(interface {
		Clone() *http.Transport
	}).Clone()
	transport.DialContext = noTimeoutDialerProxy
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, // nolint:gosec
	}
	return transport
}

var noTimeoutDialerProxy = func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   0,
		KeepAlive: 0,
	}
	return dialer.DialContext(ctx, network, addr)
}

func NewKubeFromKubeConfigFile(kubeconfig string) (*kubernetes.Clientset, error) {
	kubeConfData, err := os.ReadFile(kubeconfig)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeConfData)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func createClientSetsFromConfig(cfg *rest.Config) (*CustomClient, error) {
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	pipelineClient, err := pipelineclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	jvmbuildserviceClient, err := jvmbuildserviceclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	routeClient, err := routeclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &CustomClient{
		kubeClient:            client,
		pipelineClient:        pipelineClient,
		dynamicClient:         dynamicClient,
		jvmbuildserviceClient: jvmbuildserviceClient,
		routeClient:           routeClient,
	}, nil
}
