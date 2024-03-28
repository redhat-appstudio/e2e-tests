package integration

import (
	"context"
	"fmt"

	"github.com/devfile/library/v2/pkg/util"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	integrationv1beta1 "github.com/redhat-appstudio/integration-service/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateIntegrationTestScenarioWithEnvironment will create an IntegrationTestScenario with a
// user-supplied environment embedded in its Spec.Environment
func (i *IntegrationController) CreateIntegrationTestScenarioWithEnvironment(applicationName, namespace, gitURL, revision, pathInRepo string, environment *appservice.Environment) (*integrationv1beta1.IntegrationTestScenario, error) {
	integrationTestScenario := &integrationv1beta1.IntegrationTestScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-pass-with-env-" + util.GenerateRandomString(4),
			Namespace: namespace,
			Labels:    constants.IntegrationTestScenarioDefaultLabels,
		},
		Spec: integrationv1beta1.IntegrationTestScenarioSpec{
			Application: applicationName,
			ResolverRef: integrationv1beta1.ResolverRef{
				Resolver: "git",
				Params: []integrationv1beta1.ResolverParameter{
					{
						Name:  "url",
						Value: gitURL,
					},
					{
						Name:  "revision",
						Value: revision,
					},
					{
						Name:  "pathInRepo",
						Value: pathInRepo,
					},
				},
			},
			Environment: integrationv1beta1.TestEnvironment{
				Name: environment.Name,
				Type: environment.Spec.Type,
				Configuration: &appservice.EnvironmentConfiguration{
					Env: []appservice.EnvVarPair{
						{
							Name:  environment.Spec.Configuration.Env[0].Name,
							Value: environment.Spec.Configuration.Env[0].Value,
						},
					},
				},
			},
		},
	}

	err := i.KubeRest().Create(context.Background(), integrationTestScenario)
	if err != nil {
		return nil, fmt.Errorf("error occurred when creating the IntegrationTestScenario: %+v", err)
	}
	return integrationTestScenario, nil
}

// CreateIntegrationTestScenario creates beta1 version integrationTestScenario.
// Universal method to create a IntegrationTestScenario (its) in the kubernetes clusters and adds a suffix to the its name to allow multiple its's with unique names.
// Generate a random its name with #combinations > 11M, Create unique resource names that adhere to RFC 1123 Label Names
// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
func (i *IntegrationController) CreateIntegrationTestScenarioV2(itsName, applicationName, namespace, gitURL, revision, pathInRepo string) (*integrationv1beta1.IntegrationTestScenario, error) {
	integrationTestScenario := &integrationv1beta1.IntegrationTestScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:      itsName,
			Namespace: namespace,
			Labels:    constants.IntegrationTestScenarioDefaultLabels,
		},
		Spec: integrationv1beta1.IntegrationTestScenarioSpec{
			Application: applicationName,
			ResolverRef: integrationv1beta1.ResolverRef{
				Resolver: "git",
				Params: []integrationv1beta1.ResolverParameter{
					{
						Name:  "url",
						Value: gitURL,
					},
					{
						Name:  "revision",
						Value: revision,
					},
					{
						Name:  "pathInRepo",
						Value: pathInRepo,
					},
				},
			},
		},
	}

	err := i.KubeRest().Create(context.Background(), integrationTestScenario)
	if err != nil {
		return nil, err
	}
	return integrationTestScenario, nil
}

// CreateIntegrationTestScenario creates beta1 version integrationTestScenario.
func (i *IntegrationController) CreateIntegrationTestScenario(applicationName, namespace, gitURL, revision, pathInRepo string) (*integrationv1beta1.IntegrationTestScenario, error) {
	// use default itsName from the original function to keep backwards compatability
	itsName := "my-integration-test-" + util.GenerateRandomString(4)
	return i.CreateIntegrationTestScenarioV2(itsName, applicationName, namespace, gitURL, revision, pathInRepo)
}

// Get return the status from the Application Custom Resource object.
func (i *IntegrationController) GetIntegrationTestScenarios(applicationName, namespace string) (*[]integrationv1beta1.IntegrationTestScenario, error) {
	opts := []client.ListOption{
		client.InNamespace(namespace),
	}

	integrationTestScenarioList := &integrationv1beta1.IntegrationTestScenarioList{}
	err := i.KubeRest().List(context.Background(), integrationTestScenarioList, opts...)
	if err != nil {
		return nil, err
	}

	items := make([]integrationv1beta1.IntegrationTestScenario, 0)
	for _, t := range integrationTestScenarioList.Items {
		if t.Spec.Application == applicationName {
			items = append(items, t)
		}
	}
	return &items, nil
}

// DeleteIntegrationTestScenario removes given testScenario from specified namespace.
func (i *IntegrationController) DeleteIntegrationTestScenario(testScenario *integrationv1beta1.IntegrationTestScenario, namespace string) error {
	err := i.KubeRest().Delete(context.Background(), testScenario)
	return err
}
