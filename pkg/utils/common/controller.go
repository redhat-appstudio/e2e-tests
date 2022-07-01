package common

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-appstudio/e2e-tests/pkg/apis/github"
	kubeCl "github.com/redhat-appstudio/e2e-tests/pkg/apis/kubernetes"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Create the struct for kubernetes clients
type SuiteController struct {
	*kubeCl.K8sClient
	Github *github.Github
}

// Create controller for Application/Component crud operations
func NewSuiteController(kubeC *kubeCl.K8sClient) (*SuiteController, error) {
	// Check if a github organization env var is set, if not use by default the redhat-appstudio-qe org. See: https://github.com/redhat-appstudio-qe
	org := utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")
	token := utils.GetEnv(constants.GITHUB_TOKEN_ENV, "")
	gh := github.NewGithubClient(token, org)
	return &SuiteController{
		kubeC,
		gh,
	}, nil
}

// GetClusterTask return a clustertask object from cluster and if don't exist returns an error
func (s *SuiteController) GetClusterTask(name string, namespace string) (*v1beta1.ClusterTask, error) {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	clusterTask := &v1beta1.ClusterTask{}
	if err := s.KubeRest().Get(context.TODO(), namespacedName, clusterTask); err != nil {
		return nil, err
	}
	return clusterTask, nil
}

// ListClusterTask return a list of ClusterTasks with a specific label selectors
func (s *SuiteController) CheckIfClusterTaskExists(name string) bool {
	clusterTasks := &v1beta1.ClusterTaskList{}
	if err := s.KubeRest().List(context.TODO(), clusterTasks, &rclient.ListOptions{}); err != nil {
		return false
	}
	for _, ctasks := range clusterTasks.Items {
		if ctasks.Name == name {
			return true
		}
	}
	return false
}

// Creates a new secret in a specified namespace
func (s *SuiteController) CreateSecret(ns string, secret *corev1.Secret) (*corev1.Secret, error) {
	return s.KubeInterface().CoreV1().Secrets(ns).Create(context.TODO(), secret, metav1.CreateOptions{})
}

// Check if a secret exists, return secret and error
func (s *SuiteController) GetSecret(ns string, name string) (*corev1.Secret, error) {
	return s.KubeInterface().CoreV1().Secrets(ns).Get(context.TODO(), name, metav1.GetOptions{})
}

// Deleted a secret in a specified namespace
func (s *SuiteController) DeleteSecret(ns string, name string) error {
	return s.KubeInterface().CoreV1().Secrets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (s *SuiteController) GetPod(namespace, podName string) (*corev1.Pod, error) {
	return s.KubeInterface().CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
}

func (s *SuiteController) IsPodRunning(podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := s.GetPod(namespace, podName)
		if err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return false, fmt.Errorf("pod %q ran to completion", pod.Name)
		}
		return false, nil
	}
}

func (s *SuiteController) IsPodSuccessful(podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := s.GetPod(namespace, podName)
		if err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			return false, fmt.Errorf("pod %q has failed", pod.Name)
		}
		return false, nil
	}
}

func TaskPodExists(tr *v1beta1.TaskRun) wait.ConditionFunc {
	return func() (bool, error) {
		if tr.Status.PodName != "" {
			return true, nil
		}
		return false, nil
	}
}

func (s *SuiteController) ListPods(namespace, labelKey, labelValue string, selectionLimit int64) (*corev1.PodList, error) {
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{labelKey: labelValue}}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		Limit:         selectionLimit,
	}
	return s.KubeInterface().CoreV1().Pods(namespace).List(context.TODO(), listOptions)
}

func (s *SuiteController) WaitUntil(cond wait.ConditionFunc, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, cond)
}

func (s *SuiteController) WaitForPodSelector(
	fn func(podName, namespace string) wait.ConditionFunc, namespace, labelKey string, labelValue string,
	timeout int, selectionLimit int64) error {
	podList, err := s.ListPods(namespace, labelKey, labelValue, selectionLimit)
	if err != nil {
		return err
	}
	if len(podList.Items) == 0 {
		return fmt.Errorf("no pods in %s with label key %s and label value %s", namespace, labelKey, labelValue)
	}

	for i := range podList.Items {
		if err := s.WaitUntil(fn(podList.Items[i].Name, namespace), time.Duration(timeout)*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (s *SuiteController) GetRole(roleName, namespace string) (*rbacv1.Role, error) {
	return s.KubeInterface().RbacV1().Roles(namespace).Get(context.TODO(), roleName, metav1.GetOptions{})
}

func (s *SuiteController) GetRoleBinding(rolebindingName, namespace string) (*rbacv1.RoleBinding, error) {
	return s.KubeInterface().RbacV1().RoleBindings(namespace).Get(context.TODO(), rolebindingName, metav1.GetOptions{})
}

func (s *SuiteController) GetServiceAccount(saName, namespace string) (*corev1.ServiceAccount, error) {
	return s.KubeInterface().CoreV1().ServiceAccounts(namespace).Get(context.TODO(), saName, metav1.GetOptions{})
}

func (s *SuiteController) CreateServiceAccount(saName string, namespace string, serviceAccountSecretList []corev1.ObjectReference) (*corev1.ServiceAccount, error) {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: namespace,
		},
		Secrets: serviceAccountSecretList,
	}
	return s.KubeInterface().CoreV1().ServiceAccounts(namespace).Create(context.TODO(), serviceAccount, metav1.CreateOptions{})
}

// GetOpenshiftRoute returns the route for a given component name
func (h *SuiteController) GetOpenshiftRoute(routeName string, routeNamespace string) (*routev1.Route, error) {
	namespacedName := types.NamespacedName{
		Name:      routeName,
		Namespace: routeNamespace,
	}

	route := &routev1.Route{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, route)
	if err != nil {
		return &routev1.Route{}, err
	}
	return route, nil
}

// GetAppDeploymentByName returns the deployment for a given component name
func (h *SuiteController) GetAppDeploymentByName(deploymentName string, deploymentNamespace string) (*appsv1.Deployment, error) {
	namespacedName := types.NamespacedName{
		Name:      deploymentName,
		Namespace: deploymentNamespace,
	}

	deployment := &appsv1.Deployment{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, deployment)
	if err != nil {
		return &appsv1.Deployment{}, err
	}
	return deployment, nil
}

// GetServiceByName returns the service for a given component name
func (h *SuiteController) GetServiceByName(serviceName string, serviceNamespace string) (*corev1.Service, error) {
	namespacedName := types.NamespacedName{
		Name:      serviceName,
		Namespace: serviceNamespace,
	}

	service := &corev1.Service{}
	err := h.KubeRest().Get(context.TODO(), namespacedName, service)
	if err != nil {
		return &corev1.Service{}, err
	}
	return service, nil
}

func (s *SuiteController) CreateConfigMap(cm *corev1.ConfigMap, namespace string) (*corev1.ConfigMap, error) {
	return s.KubeInterface().CoreV1().ConfigMaps(namespace).Create(context.TODO(), cm, metav1.CreateOptions{})
}

func (s *SuiteController) GetConfigMap(name, namespace string) (*corev1.ConfigMap, error) {
	return s.KubeInterface().CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (s *SuiteController) DeleteConfigMap(name, namespace string) error {
	return s.KubeInterface().CoreV1().ConfigMaps(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (s *SuiteController) CreateRegistryAuthSecret(secretName, namespace, secretData string) (*corev1.Secret, error) {
	rawDecodedText, err := base64.StdEncoding.DecodeString(secretData)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type:       "kubernetes.io/dockerconfigjson",
		StringData: map[string]string{".dockerconfigjson": string(rawDecodedText)},
	}
	er := s.KubeRest().Create(context.TODO(), secret)
	if er != nil {
		return nil, er
	}
	return secret, nil
}

// DeleteNamespace deletes the give namespace.
func (s *SuiteController) DeleteNamespace(namespace string) error {
	_, err := s.KubeInterface().CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return fmt.Errorf("could not check for namespace existence")
	}

	return s.KubeInterface().CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
}

// CreateTestNamespace creates a namespace where Application and Component CR will be created
func (s *SuiteController) CreateTestNamespace(name string) (*corev1.Namespace, error) {

	// Check if the E2E test namespace already exists
	ns, err := s.KubeInterface().CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})

	if err != nil {
		if k8sErrors.IsNotFound(err) {
			// Create the E2E test namespace if it doesn't exist
			nsTemplate := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{constants.ArgoCDLabelKey: constants.ArgoCDLabelValue},
				}}
			ns, err = s.KubeInterface().CoreV1().Namespaces().Create(context.TODO(), &nsTemplate, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("error when creating %s namespace: %v", name, err)
			}
		} else {
			return nil, fmt.Errorf("error when getting the '%s' namespace: %v", name, err)
		}
	} else {
		// Check whether the test namespace contains correct label
		if val, ok := ns.Labels[constants.ArgoCDLabelKey]; ok && val == constants.ArgoCDLabelValue {
			return ns, nil
		}
		// Update test namespace labels in case they are missing argoCD label
		ns.Labels[constants.ArgoCDLabelKey] = constants.ArgoCDLabelValue
		ns, err = s.KubeInterface().CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("error when updating labels in '%s' namespace: %v", name, err)
		}
	}

	// "pipeline" service account needs to be present in the namespace before we start with creating tekton resources
	if err := s.WaitUntil(s.ServiceaccountPresent("pipeline", name), time.Second*30); err != nil {
		return nil, err
	}

	return ns, nil
}

func (s *SuiteController) ServiceaccountPresent(saName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := s.GetServiceAccount(saName, namespace)
		if err != nil {
			return false, nil
		}
		return true, nil
	}
}
