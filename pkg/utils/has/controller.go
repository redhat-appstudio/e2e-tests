package has

import (
	"context"
	"fmt"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	appservice "github.com/redhat-appstudio/application-service/api/v1alpha1"
	kubeCl "github.com/redhat-appstudio/e2e-tests/pkg/apis/kubernetes"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type SuiteController struct {
	*kubeCl.K8sClient
}

func NewSuiteController(kube *kubeCl.K8sClient) (*SuiteController, error) {
	return &SuiteController{
		kube,
	}, nil
}

// GetHasApplicationStatus return the status from the Application Custom Resource object
func (h *SuiteController) GetHasApplication(name, namespace string) (*appservice.Application, error) {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	application := appservice.Application{
		Spec: appservice.ApplicationSpec{},
	}
	err := h.KubeRest().Get(context.TODO(), namespacedName, &application)
	if err != nil {
		return nil, err
	}
	return &application, nil
}

// CreateHasApplication create an application Custom Resource object
func (h *SuiteController) CreateHasApplication(name, namespace string) (*appservice.Application, error) {
	application := appservice.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appservice.ApplicationSpec{
			DisplayName: name,
		},
	}
	err := h.KubeRest().Create(context.TODO(), &application)
	if err != nil {
		return nil, err
	}
	return &application, nil
}

// DeleteHasApplication delete an has application from a given name and namespace
func (h *SuiteController) DeleteHasApplication(name, namespace string) error {
	application := appservice.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return h.KubeRest().Delete(context.TODO(), &application)
}

// GetHasComponent returns the Appstudio Component Custom Resource object
func (h *SuiteController) GetHasComponent(name, namespace string) (*appservice.Component, error) {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	component := appservice.Component{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, &component)
	if err != nil {
		return nil, err
	}
	return &component, nil
}

// ScaleDeploymentReplicas scales the replicas of a given deployment
func (h *SuiteController) ScaleComponentReplicas(component *appservice.Component, replicas int) (*appservice.Component, error) {

	component.Spec.Replicas = replicas

	err := h.KubeRest().Update(context.TODO(), component, &rclient.UpdateOptions{})
	if err != nil {
		return &appservice.Component{}, err
	}
	return component, nil
}

// DeleteHasComponent delete an has component from a given name and namespace
func (h *SuiteController) DeleteHasComponent(name string, namespace string) error {
	component := appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return h.KubeRest().Delete(context.TODO(), &component)
}

// CreateComponent create an has component from a given name, namespace, application, devfile and a container image
func (h *SuiteController) CreateComponent(applicationName, componentName, namespace, gitSourceURL, containerImageSource, outputContainerImage, secret string) (*appservice.Component, error) {
	var containerImage string
	if outputContainerImage != "" {
		containerImage = outputContainerImage
	} else {
		containerImage = containerImageSource
	}
	component := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentName,
			Namespace: namespace,
		},
		Spec: appservice.ComponentSpec{
			ComponentName: componentName,
			Application:   applicationName,
			Source: appservice.ComponentSource{
				ComponentSourceUnion: appservice.ComponentSourceUnion{
					GitSource: &appservice.GitSource{
						URL: gitSourceURL,
					},
				},
			},
			Secret:         secret,
			ContainerImage: containerImage,
			Replicas:       1,
			TargetPort:     8081,
			Route:          "",
		},
	}
	err := h.KubeRest().Create(context.TODO(), component)
	if err != nil {
		return nil, err
	}
	return component, nil
}

// CreateComponentFromCDQ create a HAS Component resource from a Completed CDQ resource, which includes a stub Component CR
func (h *SuiteController) CreateComponentFromStub(compDetected appservice.ComponentDetectionDescription, componentName, namespace, secret, applicationName string) (*appservice.Component, error) {
	// The Component from the CDQ is only a template, and needs things like name filled in
	component := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentName,
			Namespace: namespace,
		},
		Spec: compDetected.ComponentStub,
	}
	component.Spec.Secret = secret
	component.Spec.Application = applicationName
	err := h.KubeRest().Create(context.TODO(), component)
	if err != nil {
		return nil, err
	}
	return component, nil
}

// DeleteHasComponent delete an has component from a given name and namespace
func (h *SuiteController) DeleteHasComponentDetectionQuery(name string, namespace string) error {
	component := appservice.ComponentDetectionQuery{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return h.KubeRest().Delete(context.TODO(), &component)
}

// CreateComponentDetectionQuery create a has componentdetectionquery from a given name, namespace, and git source
func (h *SuiteController) CreateComponentDetectionQuery(cdqName, namespace, gitSourceURL, secret string, isMultiComponent bool) (*appservice.ComponentDetectionQuery, error) {

	componentDetectionQuery := &appservice.ComponentDetectionQuery{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cdqName,
			Namespace: namespace,
		},
		Spec: appservice.ComponentDetectionQuerySpec{
			GitSource: appservice.GitSource{
				URL: gitSourceURL,
			},
			IsMultiComponent: isMultiComponent,
			Secret:           secret,
		},
	}
	err := h.KubeRest().Create(context.TODO(), componentDetectionQuery)
	if err != nil {
		return nil, err
	}
	return componentDetectionQuery, nil
}

// GetComponentDetectionQuery return the status from the ComponentDetectionQuery Custom Resource object
func (h *SuiteController) GetComponentDetectionQuery(name, namespace string) (*appservice.ComponentDetectionQuery, error) {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	componentDetectionQuery := appservice.ComponentDetectionQuery{
		Spec: appservice.ComponentDetectionQuerySpec{},
	}
	err := h.KubeRest().Get(context.TODO(), namespacedName, &componentDetectionQuery)
	if err != nil {
		return nil, err
	}
	return &componentDetectionQuery, nil
}

// GetComponentPipeline returns the pipeline for a given component labels
func (h *SuiteController) GetComponentPipelineRun(componentName, applicationName, namespace string, triggeredViaWebhook bool) (v1beta1.PipelineRun, error) {
	pipelineRunLabels := map[string]string{"build.appstudio.openshift.io/component": componentName, "build.appstudio.openshift.io/application": applicationName}
	if triggeredViaWebhook {
		pipelineRunLabels["triggers.tekton.dev/eventlistener"] = componentName
	}
	list := &v1beta1.PipelineRunList{}
	err := h.KubeRest().List(context.TODO(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(pipelineRunLabels), Namespace: namespace})

	if len(list.Items) > 0 {
		return list.Items[0], nil
	} else if len(list.Items) == 0 {
		return v1beta1.PipelineRun{}, fmt.Errorf("no pipelinerun found for component %s", componentName)
	}
	return v1beta1.PipelineRun{}, err
}

// GetEventListenerRoute returns the route for a given component name's event listener
func (h *SuiteController) GetEventListenerRoute(componentName string, componentNamespace string) (*routev1.Route, error) {
	namespacedName := types.NamespacedName{
		Name:      fmt.Sprintf("el%s", componentName),
		Namespace: componentNamespace,
	}
	route := &routev1.Route{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, route)
	if err != nil {
		return &routev1.Route{}, err
	}
	return route, nil
}

// GetComponentDeployment returns the deployment for a given component name
func (h *SuiteController) GetComponentDeployment(componentName string, componentNamespace string) (*appsv1.Deployment, error) {
	namespacedName := types.NamespacedName{
		Name:      fmt.Sprintf("el-%s", componentName),
		Namespace: componentNamespace,
	}

	deployment := &appsv1.Deployment{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, deployment)
	if err != nil {
		return &appsv1.Deployment{}, err
	}
	return deployment, nil
}

// GetComponentService returns the service for a given component name
func (h *SuiteController) GetComponentService(componentName string, componentNamespace string) (*corev1.Service, error) {
	namespacedName := types.NamespacedName{
		Name:      fmt.Sprintf("el-%s", componentName),
		Namespace: componentNamespace,
	}

	service := &corev1.Service{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, service)
	if err != nil {
		return &corev1.Service{}, err
	}
	return service, nil
}

func (h *SuiteController) WaitForComponentPipelineToBeFinished(componentName string, applicationName string, componentNamespace string) error {
	return wait.PollImmediate(20*time.Second, 15*time.Minute, func() (done bool, err error) {
		pipelineRun, _ := h.GetComponentPipelineRun(componentName, applicationName, componentNamespace, false)

		for _, condition := range pipelineRun.Status.Conditions {
			klog.Infof("PipelineRun %s reason: %s", pipelineRun.Name, condition.Reason)

			if condition.Reason == "Failed" {
				return false, fmt.Errorf("component %s pipeline failed", pipelineRun.Name)
			}

			if condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})

}

// CreateComponent create an has component from a given name, namespace, application, devfile and a container image
func (h *SuiteController) CreateComponentFromDevfile(applicationName, componentName, namespace, gitSourceURL, devfile, containerImageSource, outputContainerImage, secret string) (*appservice.Component, error) {
	var containerImage string
	if outputContainerImage != "" {
		containerImage = outputContainerImage
	} else {
		containerImage = containerImageSource
	}
	component := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentName,
			Namespace: namespace,
		},
		Spec: appservice.ComponentSpec{
			ComponentName: componentName,
			Application:   applicationName,
			Source: appservice.ComponentSource{
				ComponentSourceUnion: appservice.ComponentSourceUnion{
					GitSource: &appservice.GitSource{
						URL:        gitSourceURL,
						DevfileURL: devfile,
					},
				},
			},
			Secret:         secret,
			ContainerImage: containerImage,
			Replicas:       1,
			TargetPort:     8080,
			Route:          "",
		},
	}
	err := h.KubeRest().Create(context.TODO(), component)
	if err != nil {
		return nil, err
	}
	return component, nil
}

// Remove all components from a given repository. Usefull when create a lot of resources and want to remove all of them
func (h *SuiteController) DeleteAllComponentsInASpecificNamespace(namespace string) error {
	return h.KubeRest().DeleteAllOf(context.TODO(), &appservice.Component{}, client.InNamespace(namespace))
}

// Remove all applications from a given repository. Usefull when create a lot of resources and want to remove all of them
func (h *SuiteController) DeleteAllApplicationsInASpecificNamespace(namespace string) error {
	return h.KubeRest().DeleteAllOf(context.TODO(), &appservice.Application{}, client.InNamespace(namespace))
}

func (h *SuiteController) GetHasComponentConditionStatusMessages(name, namespace string) (messages []string, err error) {
	c, err := h.GetHasComponent(name, namespace)
	if err != nil {
		return messages, fmt.Errorf("error getting HAS component: %v", err)
	}
	for _, condition := range c.Status.Conditions {
		messages = append(messages, condition.Message)
	}
	return
}
