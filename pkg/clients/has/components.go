package has

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	. "github.com/onsi/ginkgo/v2"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/tekton"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/logs"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils/build"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	pointer "k8s.io/utils/ptr"
	"knative.dev/pkg/apis"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	RequiredLabelNotFound = "cannot retrigger PipelineRun - required label %q not found"
)

// GetComponent return a component object from kubernetes cluster
func (h *HasController) GetComponent(name string, namespace string) (*appservice.Component, error) {
	component := &appservice.Component{}
	if err := h.KubeRest().Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, component); err != nil {
		return nil, err
	}

	return component, nil
}

// GetComponentByApplicationName returns a component from kubernetes cluster given a application name.
func (h *HasController) GetComponentByApplicationName(applicationName string, namespace string) (*appservice.Component, error) {
	components := &appservice.ComponentList{}
	opts := []rclient.ListOption{
		rclient.InNamespace(namespace),
	}
	err := h.KubeRest().List(context.Background(), components, opts...)
	if err != nil {
		return nil, err
	}
	for _, component := range components.Items {
		if component.Spec.Application == applicationName {
			return &component, nil
		}
	}

	return &appservice.Component{}, fmt.Errorf("no component found %s", utils.GetAdditionalInfo(applicationName, namespace))
}

// GetComponentPipeline returns the pipeline for a given component labels
func (h *HasController) GetComponentPipelineRun(componentName string, applicationName string, namespace, sha string) (*pipeline.PipelineRun, error) {
	return h.GetComponentPipelineRunWithType(componentName, applicationName, namespace, "", sha)
}

// GetComponentPipeline returns the pipeline for a given component labels with pipeline type within label "pipelines.appstudio.openshift.io/type" ("build", "test")
func (h *HasController) GetComponentPipelineRunWithType(componentName string, applicationName string, namespace, pipelineType string, sha string) (*pipeline.PipelineRun, error) {
	pipelineRunLabels := map[string]string{"appstudio.openshift.io/component": componentName, "appstudio.openshift.io/application": applicationName}
	if pipelineType != "" {
		pipelineRunLabels["pipelines.appstudio.openshift.io/type"] = pipelineType
	}

	if sha != "" {
		pipelineRunLabels["pipelinesascode.tekton.dev/sha"] = sha
	}

	list := &pipeline.PipelineRunList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(pipelineRunLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing pipelineruns in %s namespace: %v", namespace, err)
	}

	// If we hit any other error, while fetching pipelineRun list
	if err != nil {
		return nil, fmt.Errorf("error while trying to get pipelinerun list in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return &list.Items[0], nil
	}

	return nil, fmt.Errorf("no pipelinerun found for component %s", componentName)
}

// GetAllPipelineRunsForApplication returns the pipelineruns for a given application in the namespace
func (h *HasController) GetAllPipelineRunsForApplication(applicationName, namespace string) (*pipeline.PipelineRunList, error) {
	pipelineRunLabels := map[string]string{"appstudio.openshift.io/application": applicationName}

	list := &pipeline.PipelineRunList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(pipelineRunLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing pipelineruns in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return list, nil
	}

	return nil, fmt.Errorf("no pipelinerun found for application %s", applicationName)
}

// Set of options to retrigger pipelineRuns in CI to fight against flakynes
type RetryOptions struct {
	// Indicate how many times a pipelineRun should be retriggered in case of flakines
	Retries int

	// If is set to true the PipelineRun will be retriggered always in case if pipelinerun fail for any reason. Time to time in RHTAP CI
	// we see that there are a lot of components which fail with QPS in build-container which cannot be controlled.
	// By default is false will retrigger a pipelineRun only when meet CouldntGetTask or TaskRunImagePullFailed conditions
	Always bool
}

// Waits for a given component to be finished and in case of hitting issue: https://issues.redhat.com/browse/SRVKP-2749 do a given retries.
func (h *HasController) WaitForComponentPipelineToBeFinished(component *appservice.Component, sha string, t *tekton.TektonController, r *RetryOptions) error {
	attempts := 1
	app := component.Spec.Application
	var pr *pipeline.PipelineRun

	for {
		err := wait.PollUntilContextTimeout(context.Background(), constants.PipelineRunPollingInterval, 30*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			pr, err = h.GetComponentPipelineRun(component.GetName(), app, component.GetNamespace(), sha)

			if err != nil {
				GinkgoWriter.Printf("PipelineRun has not been created yet for the Component %s/%s\n", component.GetNamespace(), component.GetName())
				return false, nil
			}

			GinkgoWriter.Printf("PipelineRun %s reason: %s\n", pr.Name, pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason())

			if !pr.IsDone() {
				return false, nil
			}

			if pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsTrue() {
				return true, nil
			}

			var prLogs string
			if err = t.StorePipelineRun(pr); err != nil {
				GinkgoWriter.Printf("failed to store PipelineRun %s:%s: %s\n", pr.GetNamespace(), pr.GetName(), err.Error())
			}
			if prLogs, err = t.GetPipelineRunLogs(pr.Name, pr.Namespace); err != nil {
				GinkgoWriter.Printf("failed to get logs for PipelineRun %s:%s: %s\n", pr.GetNamespace(), pr.GetName(), err.Error())
			}

			return false, fmt.Errorf(prLogs)
		})

		if err != nil {
			GinkgoWriter.Printf("attempt %d/%d: PipelineRun %q failed: %+v", attempts, r.Retries+1, pr.GetName(), err)
			// CouldntGetTask: Retry the PipelineRun only in case we hit the known issue https://issues.redhat.com/browse/SRVKP-2749
			// TaskRunImagePullFailed: Retry in case of https://issues.redhat.com/browse/RHTAPBUGS-985 and https://github.com/tektoncd/pipeline/issues/7184
			if attempts == r.Retries+1 || (!r.Always && pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason() != "CouldntGetTask" && pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason() != "TaskRunImagePullFailed") {
				return err
			}

			if sha, err = h.RetriggerComponentPipelineRun(component, pr); err != nil {
				return fmt.Errorf("unable to retrigger component %s:%s: %+v", component.GetNamespace(), component.GetName(), err)
			}
			attempts++
		} else {
			break
		}
	}

	return nil
}

// Universal method to create a component in the kubernetes clusters and adds a suffix to the component name to allow multiple components with unique names.
// Generate a random component name with #combinations > 11M, Create unique resource names that adhere to RFC 1123 Label Names
// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
func (h *HasController) CreateComponentV2(componentSpec appservice.ComponentSpec, namespace string, outputContainerImage string, secret string, applicationName string, skipInitialChecks bool, annotations map[string]string, option string) (*appservice.Component, string, error) {
	var componentName string

	if option == "default" {
		componentName = componentSpec.ComponentName
	} else {
		componentName = componentSpec.ComponentName + "-" + util.GenerateRandomString(5)
	}

	componentObject := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			// adding default label because of the BuildPipelineSelector in build test
			Labels:    constants.ComponentDefaultLabel,
			Name:      componentName,
			Namespace: namespace,
			Annotations: map[string]string{
				"skip-initial-checks": strconv.FormatBool(skipInitialChecks),
			},
		},
		Spec: componentSpec,
	}
	componentObject.Spec.Secret = secret
	componentObject.Spec.Application = applicationName

	if len(annotations) > 0 {
		componentObject.Annotations = utils.MergeMaps(componentObject.Annotations, annotations)

	}

	if componentObject.Spec.TargetPort == 0 {
		componentObject.Spec.TargetPort = 8081
	}
	if outputContainerImage != "" {
		componentObject.Spec.ContainerImage = outputContainerImage
	} else if componentObject.Annotations["image.redhat.com/generate"] == "" {
		// Generate default public image repo since nothing is mentioned specifically
		componentObject.Annotations = utils.MergeMaps(componentObject.Annotations, constants.ImageControllerAnnotationRequestPublicRepo)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*1)
	defer cancel()
	if err := h.KubeRest().Create(ctx, componentObject); err != nil {
		return nil, componentName, err
	}
	if err := utils.WaitUntil(h.ComponentReady(componentObject), time.Minute*10); err != nil {
		componentObject = h.refreshComponentForErrorDebug(componentObject)
		return nil, componentName, fmt.Errorf("timed out when waiting for component %s to be ready in %s namespace. component: %s", componentName, namespace, utils.ToPrettyJSONString(componentObject))
	}

	if utils.WaitUntil(h.CheckForImageAnnotation(componentObject), time.Minute*5) != nil {
		componentObject = h.refreshComponentForErrorDebug(componentObject)
		return nil, componentName, fmt.Errorf("timed out when waiting for image-controller annotations to be updated on component %s in namespace %s. component: %s", componentName, namespace, utils.ToPrettyJSONString(componentObject))
	}
	return componentObject, componentName, nil
}

// Universal method to create a component in the kubernetes clusters.
func (h *HasController) CreateComponent(componentSpec appservice.ComponentSpec, namespace string, outputContainerImage string, secret string, applicationName string, skipInitialChecks bool, annotations map[string]string) (*appservice.Component, error) {
	componentObject, _, err := h.CreateComponentV2(componentSpec, namespace, outputContainerImage, secret, applicationName, skipInitialChecks, annotations, "default")
	return componentObject, err
}

// CreateComponentWithDockerSource creates a component based on container image source.
func (h *HasController) CreateComponentWithDockerSource(applicationName, componentName, namespace, gitSourceURL, containerImageSource, outputContainerImage, secret string) (*appservice.Component, error) {
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
						URL:           gitSourceURL,
						DockerfileURL: containerImageSource,
					},
				},
			},
			Secret:         secret,
			ContainerImage: outputContainerImage,
			Replicas:       pointer.To[int](1),
			TargetPort:     8081,
			Route:          "",
		},
	}
	err := h.KubeRest().Create(context.Background(), component)
	if err != nil {
		return nil, err
	}
	return component, nil
}

// ScaleDeploymentReplicas scales the replicas of a given deployment
func (h *HasController) ScaleComponentReplicas(component *appservice.Component, replicas *int) (*appservice.Component, error) {
	component.Spec.Replicas = replicas

	err := h.KubeRest().Update(context.Background(), component, &rclient.UpdateOptions{})
	if err != nil {
		return &appservice.Component{}, err
	}
	return component, nil
}

// DeleteComponent delete an has component from a given name and namespace
func (h *HasController) DeleteComponent(name string, namespace string, reportErrorOnNotFound bool) error {
	// temporary logs
	start := time.Now()
	GinkgoWriter.Printf("Start to delete component '%s' at %s\n", name, start.Format(time.RFC3339))

	component := appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := h.KubeRest().Delete(context.Background(), &component); err != nil {
		if !k8sErrors.IsNotFound(err) || (k8sErrors.IsNotFound(err) && reportErrorOnNotFound) {
			return fmt.Errorf("error deleting a component: %+v", err)
		}
	}

	// RHTAPBUGS-978: temporary timeout to 15min
	err := utils.WaitUntil(h.ComponentDeleted(&component), 15*time.Minute)

	// temporary logs
	deletionTime := time.Since(start).Minutes()
	GinkgoWriter.Printf("Finish to delete component '%s' at %s. It took '%f' minutes\n", name, time.Now().Format(time.RFC3339), deletionTime)

	return err
}

// DeleteAllComponentsInASpecificNamespace removes all component CRs from a specific namespace. Useful when creating a lot of resources and want to remove all of them
func (h *HasController) DeleteAllComponentsInASpecificNamespace(namespace string, timeout time.Duration) error {
	// temporary logs
	start := time.Now()
	GinkgoWriter.Println("Start to delete all components in namespace '%s' at %s", namespace, start.String())

	if err := h.KubeRest().DeleteAllOf(context.Background(), &appservice.Component{}, rclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("error deleting components from the namespace %s: %+v", namespace, err)
	}

	componentList := &appservice.ComponentList{}

	err := utils.WaitUntil(func() (done bool, err error) {
		if err := h.KubeRest().List(context.Background(), componentList, &rclient.ListOptions{Namespace: namespace}); err != nil {
			return false, nil
		}
		return len(componentList.Items) == 0, nil
	}, timeout)

	// temporary logs
	deletionTime := time.Since(start).Minutes()
	GinkgoWriter.Println("Finish to delete all components in namespace '%s' at %s. It took '%f' minutes", namespace, time.Now().Format(time.RFC3339), deletionTime)

	return err
}

// Waits for a component to be reconciled in the application service.
func (h *HasController) ComponentReady(component *appservice.Component) wait.ConditionFunc {
	return func() (bool, error) {
		messages, err := h.GetComponentConditionStatusMessages(component.Name, component.Namespace)
		if err != nil {
			return false, nil
		}
		for _, m := range messages {
			if strings.Contains(m, "success") {
				return true, nil
			}
		}
		return false, nil
	}
}

// Waits for a component until is deleted and if not will return an error
func (h *HasController) ComponentDeleted(component *appservice.Component) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := h.GetComponent(component.Name, component.Namespace)
		return err != nil && k8sErrors.IsNotFound(err), nil
	}
}

// Get the message from the status of a component. Usefull for debugging purposes.
func (h *HasController) GetComponentConditionStatusMessages(name, namespace string) (messages []string, err error) {
	c, err := h.GetComponent(name, namespace)
	if err != nil {
		return messages, fmt.Errorf("error getting HAS component: %v", err)
	}
	for _, condition := range c.Status.Conditions {
		messages = append(messages, condition.Message)
	}
	return
}

// Universal method to retrigger pipelineruns in kubernetes cluster
func (h *HasController) RetriggerComponentPipelineRun(component *appservice.Component, pr *pipeline.PipelineRun) (sha string, err error) {
	if err = h.KubeRest().Delete(context.Background(), pr); err != nil {
		return "", fmt.Errorf("failed to delete PipelineRun %q from %q namespace with error: %v", pr.GetName(), pr.GetNamespace(), err)
	}

	prLabels := pr.GetLabels()
	// In case of PipelineRun managed by PaC we are able to retrigger the pipeline only
	// by updating the related branch
	if prLabels["app.kubernetes.io/managed-by"] == "pipelinesascode.tekton.dev" {
		var ok bool
		var repoName, eventType, branchName string
		pacRepoNameLabelName := "pipelinesascode.tekton.dev/url-repository"
		pacEventTypeLabelName := "pipelinesascode.tekton.dev/event-type"
		componentLabelName := "appstudio.openshift.io/component"
		targetBranchAnnotationName := "build.appstudio.redhat.com/target_branch"

		if repoName, ok = prLabels[pacRepoNameLabelName]; !ok {
			return "", fmt.Errorf(RequiredLabelNotFound, pacRepoNameLabelName)
		}
		if eventType, ok = prLabels[pacEventTypeLabelName]; !ok {
			return "", fmt.Errorf(RequiredLabelNotFound, pacEventTypeLabelName)
		}
		// PipelineRun is triggered from a pull request, need to update the PaC PR source branch
		if eventType == "pull_request" {
			if len(prLabels[componentLabelName]) < 1 {
				return "", fmt.Errorf(RequiredLabelNotFound, componentLabelName)
			}
			branchName = constants.PaCPullRequestBranchPrefix + prLabels[componentLabelName]
		} else {
			// No straightforward way to get a target branch from PR labels -> using annotation
			if branchName, ok = pr.GetAnnotations()[targetBranchAnnotationName]; !ok {
				return "", fmt.Errorf("cannot retrigger PipelineRun - required annotation %q not found", targetBranchAnnotationName)
			}
		}
		file, err := h.Github.CreateFile(repoName, util.GenerateRandomString(5), "test", branchName)
		if err != nil {
			return "", fmt.Errorf("failed to retrigger PipelineRun %s in %s namespace: %+v", pr.GetName(), pr.GetNamespace(), err)
		}
		sha = file.GetSHA()

		// To retrigger simple build PipelineRun we just need to update the initial build annotation
		// in Component CR
	} else {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			component, err := h.GetComponent(component.GetName(), component.GetNamespace())
			if err != nil {
				return fmt.Errorf("failed to get component for PipelineRun %q in %q namespace: %+v", pr.GetName(), pr.GetNamespace(), err)
			}
			component.Annotations = utils.MergeMaps(component.Annotations, constants.ComponentTriggerSimpleBuildAnnotation)
			if err = h.KubeRest().Update(context.Background(), component); err != nil {
				return fmt.Errorf("failed to update Component %q in %q namespace", component.GetName(), component.GetNamespace())
			}
			return err
		})

		if err != nil {
			return "", err
		}
	}
	watch, err := h.PipelineClient().TektonV1().PipelineRuns(component.GetNamespace()).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("error when initiating watch for new PipelineRun after retriggering it for component %s:%s", component.GetNamespace(), component.GetName())
	}
	newPRFound := false
	for {
		select {
		case <-time.After(5 * time.Minute):
			return "", fmt.Errorf("timed out waiting for new PipelineRun to appear after retriggering it for component %s:%s", component.GetNamespace(), component.GetName())
		case event := <-watch.ResultChan():
			if event.Object == nil {
				continue
			}
			newPR, ok := event.Object.(*pipeline.PipelineRun)
			if !ok {
				continue
			}
			if pr.GetName() != newPR.GetName() {
				newPRFound = true
			}
		}
		if newPRFound {
			break
		}
	}

	return sha, nil
}

// refreshComponentForErrorDebug returns the latest component object from the kubernetes cluster.
func (h *HasController) refreshComponentForErrorDebug(component *appservice.Component) *appservice.Component {
	retComp := &appservice.Component{}
	key := rclient.ObjectKeyFromObject(component)
	err := h.KubeRest().Get(context.Background(), key, retComp)
	if err != nil {
		//TODO let's log this somehow, but return the original component obj, as that is better than nothing
		return component
	}
	return retComp
}

func (h *HasController) CheckForImageAnnotation(component *appservice.Component) wait.ConditionFunc {
	return func() (bool, error) {
		componentCR, err := h.GetComponent(component.Name, component.Namespace)
		if err != nil {
			klog.Errorf("failed to get component %s with error: %+v", component.Name, err)
			return false, nil
		}
		annotations := componentCR.GetAnnotations()
		return build.IsImageAnnotationPresent(annotations) && build.ImageRepoCreationSucceeded(annotations), nil
	}
}

// Gets value of a specified annotation in a component
func (h *HasController) GetComponentAnnotation(componentName, annotationKey, namespace string) (string, error) {
	component, err := h.GetComponent(componentName, namespace)
	if err != nil {
		return "", fmt.Errorf("error when getting component: %+v", err)
	}
	return component.Annotations[annotationKey], nil
}

// Sets annotation in a component
func (h *HasController) SetComponentAnnotation(componentName, annotationKey, annotationValue, namespace string) error {
	component, err := h.GetComponent(componentName, namespace)
	if err != nil {
		return fmt.Errorf("error when getting component: %+v", err)
	}
	newAnnotations := component.GetAnnotations()
	newAnnotations[annotationKey] = annotationValue
	component.SetAnnotations(newAnnotations)
	err = h.KubeRest().Update(context.Background(), component)
	if err != nil {
		return fmt.Errorf("error when updating component: %+v", err)
	}
	return nil
}

// StoreComponent stores a given Component as an artifact.
func (h *HasController) StoreComponent(component *appservice.Component) error {
	artifacts := make(map[string][]byte)

	componentConditionStatus, err := h.GetComponentConditionStatusMessages(component.Name, component.Namespace)
	if err != nil {
		return err
	}
	artifacts["component-condition-status-"+component.Name+".log"] = []byte(strings.Join(componentConditionStatus, "\n"))

	componentYaml, err := yaml.Marshal(component)
	if err != nil {
		return err
	}
	artifacts["component-"+component.Name+".yaml"] = componentYaml

	if err := logs.StoreArtifacts(artifacts); err != nil {
		return err
	}

	return nil
}

// StoreAllComponents stores all Components in a given namespace.
func (h *HasController) StoreAllComponents(namespace string) error {
	componentList := &appservice.ComponentList{}
	if err := h.KubeRest().List(context.Background(), componentList, &rclient.ListOptions{Namespace: namespace}); err != nil {
		return err
	}

	for _, component := range componentList.Items {
		if err := h.StoreComponent(&component); err != nil {
			return err
		}
	}
	return nil
}

// specific for tests/remote-secret/image-repository-cr-image-pull-remote-secret.go
func (h *HasController) CreateComponentWithoutGenerateAnnotation(componentSpec appservice.ComponentSpec, namespace string, secret string, applicationName string, skipInitialChecks bool) (*appservice.Component, error) {
	componentObject := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			// adding default label because of the BuildPipelineSelector in build test
			Labels:    constants.ComponentDefaultLabel,
			Name:      componentSpec.ComponentName,
			Namespace: namespace,
			Annotations: map[string]string{
				"skip-initial-checks": strconv.FormatBool(skipInitialChecks),
			},
		},
		Spec: componentSpec,
	}
	componentObject.Spec.Secret = secret
	componentObject.Spec.Application = applicationName

	if componentObject.Spec.TargetPort == 0 {
		componentObject.Spec.TargetPort = 8081
	}

	if err := h.KubeRest().Create(context.Background(), componentObject); err != nil {
		return nil, err
	}

	if err := utils.WaitUntil(h.ComponentReady(componentObject), time.Minute*10); err != nil {
		componentObject = h.refreshComponentForErrorDebug(componentObject)
		return nil, fmt.Errorf("timed out when waiting for component %s to be ready in %s namespace. component: %s", componentSpec.ComponentName, namespace, utils.ToPrettyJSONString(componentObject))
	}

	return componentObject, nil
}
