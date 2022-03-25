package tekton

import (
	"fmt"
	"strings"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This is a demo task to create test image and task signing
func kanikoTaskRun(image string) *v1beta1.TaskRun {
	imageInfo := strings.Split(image, "/")
	namespace := imageInfo[1]
	imageName := imageInfo[2]

	return &v1beta1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("kaniko-taskrun-%s", imageName),
			Namespace:    namespace,
		},
		Spec: v1beta1.TaskRunSpec{
			Params: []v1beta1.Param{
				{
					Name: "IMAGE",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: image,
					},
				},
			},
			TaskRef: &v1beta1.TaskRef{
				Kind:   v1beta1.NamespacedTaskKind,
				Name:   "kaniko-chains",
				Bundle: "quay.io/redhat-appstudio/appstudio-tasks:a6fea8de3e3285ec7675245958db3e3673243d59-1",
			},
			Workspaces: []v1beta1.WorkspaceBinding{
				{
					Name:     "source",
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
	}
}

// image is full url to the image
// Example image: image-registry.openshift-image-registry.svc:5000/tekton-chains/kaniko-chains
func verifyTaskRun(image, taskName string) *v1beta1.TaskRun {
	imageInfo := strings.Split(image, "/")
	namespace := imageInfo[1]
	imageName := imageInfo[2]

	return &v1beta1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s", taskName, imageName),
			Namespace:    namespace,
		},
		Spec: v1beta1.TaskRunSpec{
			Params: []v1beta1.Param{
				{
					Name: "IMAGE",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: image,
					},
				},
				{
					Name: "PUBLIC_KEY",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: "k8s://tekton-chains/signing-secrets",
					},
				},
			},
			TaskRef: &v1beta1.TaskRef{
				Kind:   v1beta1.NamespacedTaskKind,
				Name:   taskName,
				Bundle: "quay.io/redhat-appstudio/appstudio-tasks:a6fea8de3e3285ec7675245958db3e3673243d59-1",
			},
		},
	}
}
