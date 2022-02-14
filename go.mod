module github.com/redhat-appstudio/e2e-tests

go 1.16

require (
	github.com/argoproj/argo-cd/v2 v2.1.7
	github.com/argoproj/gitops-engine v0.4.1
	github.com/google/uuid v1.3.0
	github.com/onsi/ginkgo/v2 v2.1.1
	github.com/onsi/gomega v1.18.1
	github.com/openshift/api v0.0.0-20200930075302-db52bc4ef99f
	github.com/redhat-appstudio/application-service v0.0.0-20220209224245-a3c147c351d4
	github.com/tektoncd/pipeline v0.30.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.4
	k8s.io/client-go v11.0.1-0.20190816222228-6d55c1b1f1ca+incompatible
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/controller-runtime v0.9.5
)

replace (
	github.com/go-redis/redis/v8 => github.com/go-redis/redis/v8 v8.11.4 // indirect
	k8s.io/api => k8s.io/api v0.21.4
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.4
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.4
	k8s.io/apiserver => k8s.io/apiserver v0.21.4
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.4
	k8s.io/client-go => k8s.io/client-go v0.21.4
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.4
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.4
	k8s.io/code-generator => k8s.io/code-generator v0.21.4
	k8s.io/component-base => k8s.io/component-base v0.21.4
	k8s.io/component-helpers => k8s.io/component-helpers v0.21.4
	k8s.io/controller-manager => k8s.io/controller-manager v0.21.4
	k8s.io/cri-api => k8s.io/cri-api v0.21.4
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.4
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.4
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.4
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.4
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.4
	k8s.io/kubectl => k8s.io/kubectl v0.21.4
	k8s.io/kubelet => k8s.io/kubelet v0.21.4
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.4
	k8s.io/metrics => k8s.io/metrics v0.21.4
	k8s.io/mount-utils => k8s.io/mount-utils v0.21.4
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.4
)
