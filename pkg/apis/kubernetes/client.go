package client

import (
	routev1 "github.com/openshift/api/route/v1"
	applicationservice "github.com/redhat-appstudio/application-service/api/v1alpha1"
	gitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/appstudio-shared/apis/appstudio.redhat.com/v1alpha1"
	managedgitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/backend/apis/managed-gitops/v1alpha1"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	ecp "github.com/hacbs-contract/enterprise-contract-controller/api/v1alpha1"
	integrationservice "github.com/redhat-appstudio/integration-service/api/v1alpha1"
	jvmbuildservice "github.com/redhat-appstudio/jvm-build-service/pkg/apis/jvmbuildservice/v1alpha1"
	jvmbuildserviceclientset "github.com/redhat-appstudio/jvm-build-service/pkg/client/clientset/versioned"
	release "github.com/redhat-appstudio/release-service/api/v1alpha1"
	spi "github.com/redhat-appstudio/service-provider-integration-operator/api/v1beta1"
	tekton "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	pipelineclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type K8sClient struct {
	kubeClient     *kubernetes.Clientset
	crClient       crclient.Client
	pipelineClient pipelineclientset.Interface
	jvmbuildserviceClient jvmbuildserviceclientset.Interface
}

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(applicationservice.AddToScheme(scheme))
	utilruntime.Must(tekton.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(managedgitopsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(spi.AddToScheme(scheme))
	utilruntime.Must(toolchainv1alpha1.AddToScheme(scheme))
	utilruntime.Must(release.AddToScheme(scheme))
	utilruntime.Must(gitopsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(integrationservice.AddToScheme(scheme))
	utilruntime.Must(jvmbuildservice.AddToScheme(scheme))
	utilruntime.Must(ecp.AddToScheme(scheme))
}

// Kube returns the clientset for Kubernetes upstream.
func (c *K8sClient) KubeInterface() kubernetes.Interface {
	return c.kubeClient
}

// Return a rest client to perform CRUD operations on Kubernetes objects
func (c *K8sClient) KubeRest() crclient.Client {
	return c.crClient
}

func (c *K8sClient) PipelineClient() pipelineclientset.Interface {
	return c.pipelineClient
}

func (c *K8sClient) JvmbuildserviceClient() jvmbuildserviceclientset.Interface {
	return c.jvmbuildserviceClient
}

// NewHASClient creates kubernetes client wrapper
func NewK8SClient() (*K8sClient, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	crClient, err := crclient.New(cfg, crclient.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	pipelineClient, err := pipelineclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	jvmbildserviceClient, err := jvmbuildserviceclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &K8sClient{
		kubeClient:     client,
		crClient:       crClient,
		pipelineClient: pipelineClient,
		jvmbuildserviceClient: jvmbildserviceClient,
	}, nil
}
