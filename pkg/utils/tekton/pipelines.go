package tekton

import (
	"fmt"
	"strings"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PipelineRunGenerator interface {
	Generate() *v1beta1.PipelineRun
}

type BuildahDemo struct {
	Image  string
	Bundle string
}

// This is a demo pipeline to create test image and task signing
func (g BuildahDemo) Generate() *v1beta1.PipelineRun {
	imageInfo := strings.Split(g.Image, "/")
	namespace := imageInfo[1]
	// Make the PipelineRun name predictable.
	name := imageInfo[2]

	return &v1beta1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1beta1.PipelineRunSpec{
			Params: []v1beta1.Param{
				{
					Name:  "dockerfile",
					Value: *v1beta1.NewArrayOrString("Dockerfile"),
				},
				{
					Name:  "output-image",
					Value: *v1beta1.NewArrayOrString(g.Image),
				},
				{
					Name:  "git-url",
					Value: *v1beta1.NewArrayOrString("https://github.com/ziwoshixianzhe/simple_docker_app.git"),
				},
			},
			PipelineRef: &v1beta1.PipelineRef{
				Name:   "docker-build",
				Bundle: g.Bundle,
			},
			Workspaces: []v1beta1.WorkspaceBinding{
				{
					Name: "workspace",
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "app-studio-default-workspace",
					},
				},
			},
		},
	}
}

type VerifyEnterpriseContract struct {
	PipelineRunName string
	ImageRef        string
	PublicSecret    string
	PipelineName    string
	RekorHost       string
	SslCertDir      string
	StrictPolicy    string
	Bundle          string
}

func (t VerifyEnterpriseContract) Generate() *v1beta1.PipelineRun {
	imageInfo := strings.Split(t.ImageRef, "/")
	namespace := imageInfo[1]

	return &v1beta1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", t.PipelineRunName),
			Namespace:    namespace,
		},
		Spec: v1beta1.PipelineRunSpec{
			Params: []v1beta1.Param{
				{
					Name: "IMAGE_REF",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: t.ImageRef,
					},
				},
				{
					Name: "PIPELINERUN_NAME",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: t.PipelineName,
					},
				},
				{
					Name: "PUBLIC_KEY",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: t.PublicSecret,
					},
				},
				{
					Name: "REKOR_HOST",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: t.RekorHost,
					},
				},
				{
					Name: "SSL_CERT_DIR",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: t.SslCertDir,
					},
				},
				{
					Name: "STRICT_POLICY",
					Value: v1beta1.ArrayOrString{
						Type:      v1beta1.ParamTypeString,
						StringVal: t.StrictPolicy,
					},
				},
			},
			PipelineRef: &v1beta1.PipelineRef{
				Name:   "e2e-ec",
				Bundle: t.Bundle,
			},
		},
	}
}
