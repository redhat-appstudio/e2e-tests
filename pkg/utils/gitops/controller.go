package gitops

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	routev1 "github.com/openshift/api/route/v1"
	kubeCl "github.com/redhat-appstudio/e2e-tests/pkg/apis/kubernetes"
	managedgitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/backend/apis/managed-gitops/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SuiteController struct {
	*kubeCl.K8sClient
}

func NewSuiteController(kube *kubeCl.K8sClient) (*SuiteController, error) {
	return &SuiteController{
		kube,
	}, nil
}

func (h *SuiteController) CreateGitOpsCR(name string, namespace string, repoUrl string, repoPath string, repoRevision string) (*managedgitopsv1alpha1.GitOpsDeployment, error) {
	gitOpsDeployment := &managedgitopsv1alpha1.GitOpsDeployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name,
			Namespace:    namespace,
		},
		Spec: managedgitopsv1alpha1.GitOpsDeploymentSpec{
			Source: managedgitopsv1alpha1.ApplicationSource{
				RepoURL:        repoUrl,
				Path:           repoPath,
				TargetRevision: repoRevision,
			},
			Type: managedgitopsv1alpha1.GitOpsDeploymentSpecType_Automated,
		},
	}

	err := h.KubeRest().Create(context.TODO(), gitOpsDeployment)
	if err != nil {
		return nil, err
	}
	return gitOpsDeployment, nil
}

// DeleteGitOpsDeployment deletes an gitops deployment from a given name and namespace
func (h *SuiteController) DeleteGitOpsCR(name string, namespace string) error {
	gitOpsDeployment := &managedgitopsv1alpha1.GitOpsDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return h.KubeRest().Delete(context.TODO(), gitOpsDeployment)
}

// GetGitOpsDeployedImage return the image used by the given component deployment
func (h *SuiteController) GetGitOpsDeployedImage(deployment *appsv1.Deployment) (string, error) {
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		return deployment.Spec.Template.Spec.Containers[0].Image, nil
	} else {
		return "", fmt.Errorf("error when getting the deployed image")
	}
}

// Checks that the deployed backend component is actually reachable and returns 200
func (h *SuiteController) CheckGitOpsEndpoint(route *routev1.Route, endpoint string) error {
	if len(route.Spec.Host) > 0 {
		routeUrl := "https://" + route.Spec.Host + endpoint

		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		resp, err := http.Get(routeUrl)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("route responded with '%d' status code", resp.StatusCode)
		}
	} else {
		return fmt.Errorf("route is invalid: '%s'", route.Spec.Host)
	}

	return nil
}

// Remove all tokens from a given repository. Usefull when create a lot of resources and want to remove all of them
func (h *SuiteController) DeleteAllGitOpsDeploymentInASpecificNamespace(namespace string) error {
	return h.KubeRest().DeleteAllOf(context.TODO(), &managedgitopsv1alpha1.GitOpsDeployment{}, client.InNamespace(namespace))
}
