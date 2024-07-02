package gitops

import (
	"context"
	"fmt"

	codereadytoolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSpaceRequests returns a list of spaceRequests in given namespace.
func (g *GitopsController) GetSpaceRequests(namespace string) (*codereadytoolchainv1alpha1.SpaceRequestList, error) {
	spaceRequestList := &codereadytoolchainv1alpha1.SpaceRequestList{}

	opts := []client.ListOption{
		client.InNamespace(namespace),
	}

	err := g.KubeRest().List(context.Background(), spaceRequestList, opts...)
	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error occurred while trying to list spaceRequests in %s namespace: %v", namespace, err)
	}

	return spaceRequestList, nil
}

// GetSpaceRequest returns spaceRequest if the name exists in the namespace
func (g *GitopsController) GetSpaceRequest(namespace, name string) (*codereadytoolchainv1alpha1.SpaceRequest, error) {
	spaceRequest := &codereadytoolchainv1alpha1.SpaceRequest{}

	err := g.KubeRest().Get(context.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, spaceRequest)

	return spaceRequest, err
}
