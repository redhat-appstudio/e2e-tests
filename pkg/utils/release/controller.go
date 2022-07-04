package release

import (
	"context"

	kubeCl "github.com/redhat-appstudio/e2e-tests/pkg/apis/kubernetes"
	gitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/appstudio-shared/apis/appstudio.redhat.com/v1alpha1"
	"github.com/redhat-appstudio/release-service/api/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

// CreateApplicationSnapshot creates a new ApplicationSnapshot using the given parameters.
func (s *SuiteController) CreateApplicationSnapshot(name string, namespace string, applicationName string, snapshotComponents []gitopsv1alpha1.ApplicationSnapshotComponent) (*gitopsv1alpha1.ApplicationSnapshot, error) {
	applicationSnapshot := &gitopsv1alpha1.ApplicationSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gitopsv1alpha1.ApplicationSnapshotSpec{
			Application: applicationName,
			Components:  snapshotComponents,
		},
	}

	return applicationSnapshot, s.KubeRest().Create(context.TODO(), applicationSnapshot)
}

// CreateRelease creates a new Release using the given parameters.
func (s *SuiteController) CreateRelease(name, namespace, snapshot, sourceReleaseLink string) (*v1alpha1.Release, error) {
	release := &v1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ReleaseSpec{
			ApplicationSnapshot: snapshot,
			ReleaseLink:         sourceReleaseLink,
		},
	}

	return release, s.KubeRest().Create(context.TODO(), release)
}

// CreateReleaseLink creates a new ReleaseLink using the given parameters.
func (s *SuiteController) CreateReleaseLink(name, namespace, application, targetNamespace, releaseStrategy string) (*v1alpha1.ReleaseLink, error) {
	releaseLink := &v1alpha1.ReleaseLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ReleaseLinkSpec{
			DisplayName:     name,
			Application:     application,
			Target:          targetNamespace,
			ReleaseStrategy: releaseStrategy,
		},
	}

	return releaseLink, s.KubeRest().Create(context.TODO(), releaseLink)
}

// GetPipelineRunInNamespace returns the Release PipelineRun referencing the given release.
func (s *SuiteController) GetPipelineRunInNamespace(namespace, releaseName, releaseNamespace string) (*v1beta1.PipelineRun, error) {
	pipelineRuns := &v1beta1.PipelineRunList{}
	opts := []client.ListOption{
		client.MatchingLabels{
			"release.appstudio.openshift.io/name":      releaseName,
			"release.appstudio.openshift.io/workspace": releaseNamespace,
		},
		client.InNamespace(namespace),
	}

	err := s.KubeRest().List(context.TODO(), pipelineRuns, opts...)

	if err == nil && len(pipelineRuns.Items) > 0 {
		return &pipelineRuns.Items[0], nil
	}

	return nil, err
}

// GetRelease returns the release with the given name in the given namespace.
func (s *SuiteController) GetRelease(releaseName, releaseNamespace string) (*v1alpha1.Release, error) {
	release := &v1alpha1.Release{}

	err := s.KubeRest().Get(context.TODO(), types.NamespacedName{
		Name:      releaseName,
		Namespace: releaseNamespace,
	}, release)

	return release, err
}

// Get ReleaseLink object from a given namespace
func (s *SuiteController) GetReleaseLink(name string, namespace string) (*v1alpha1.ReleaseLink, error) {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	releaseLink := &v1alpha1.ReleaseLink{}
	err := s.KubeRest().Get(context.TODO(), namespacedName, releaseLink)
	if err != nil {
		return nil, err
	}
	return releaseLink, nil
}

// CreateReleaseStrategy creates a new ReleaseStrategy using the given parameters.
func (s *SuiteController) CreateReleaseStrategy(name, namespace, pipelineName, bundle, policy, serviceAccountName string, releaseStrategyParams []v1alpha1.Params) (*v1alpha1.ReleaseStrategy, error) {
	releaseStrategy := &v1alpha1.ReleaseStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ReleaseStrategySpec{
			Pipeline:       pipelineName,
			Bundle:         bundle,
			Policy:         policy,
			Params:         releaseStrategyParams,
			ServiceAccount: serviceAccountName,
		},
	}
	return releaseStrategy, s.KubeRest().Create(context.TODO(), releaseStrategy)
}
