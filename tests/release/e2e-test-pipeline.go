package release

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/release-service/api/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"knative.dev/pkg/apis"
)

const (
	releasePvcName          = "release-pvc"
	serviceAccountName      = "m5-service-account"
	secretName              = "hacbs-release-tests-token"
	volumeAccessModeForTest = "ReadWriteMany"
)

var releaseStartegyParams = []v1alpha1.Params{
	{Name: "extraConfigGitUrl", Value: "https://github.com/davidmogar/strategy-configs.git"},
	{Name: "extraConfigPath", Value: "m5.yaml"},
	{Name: "extraConfigRevision", Value: "main"},
}

var serviceAccountSecretList = []corev1.ObjectReference{
	{
		Name: serviceAccountName,
	},
}

var _ = framework.ReleaseSuiteDescribe("test-demo", func() {
	defer GinkgoRecover()
	// Initialize the tests controllers
	framework, err := framework.NewFramework()
	Expect(err).NotTo(HaveOccurred())

	var devNamespace = uuid.New().String()
	var managedNamespace = uuid.New().String()

	BeforeAll(func() {
		// Create the dev namespace
		demo, err := framework.CommonController.CreateTestNamespace(devNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating namespace '%s': %v", demo.Name, err)

		// Create the managed namespace
		namespace, err := framework.CommonController.CreateTestNamespace(managedNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating namespace '%s': %v", namespace.Name, err)
	})

	AfterAll(func() {
		// Delete the dev and managed namespaces with all the resources created in them
		Expect(framework.CommonController.DeleteNamespace(devNamespace)).NotTo(HaveOccurred())
		Expect(framework.CommonController.DeleteNamespace(managedNamespace)).NotTo(HaveOccurred())
	})

	var _ = Describe("Creation of the 'tekton test-bundle e2e-test' resources", func() {
		It("Create PVC in", func() {
			pvcs := framework.TektonController.KubeInterface().CoreV1().PersistentVolumeClaims(managedNamespace)
			_, err := framework.TektonController.CreatePVC(pvcs, releaseName, volumeAccessModeForTest, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create Release Strategy", func() {
			_, err := framework.ReleaseController.CreateReleaseStrategy(releaseStrategyName, managedNamespace, releasePipelineName, releasePipelineBundle, releaseStrategyPolicy, releaseStartegyParams, serviceAccountName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create Release Link in dev namespace", func() {
			_, err := framework.ReleaseController.CreateReleaseLink(sourceReleaseLinkName, devNamespace, applicationName, managedNamespace, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create Release Link in managed namespace", func() {
			_, err := framework.ReleaseController.CreateReleaseLink(targetReleaseLinkName, managedNamespace, applicationName, devNamespace, releaseStrategyName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create a Release", func() {
			_, err := framework.CommonController.CreateServiceAccount(serviceAccountName, managedNamespace, serviceAccountSecretList)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	var _ = Describe("Post-release verification", func() {
		It("A PipelineRun should have been created in the managed namespace", func() {
			Eventually(func() error {
				_, err := framework.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseName, devNamespace)

				return err
			}, 1*time.Minute, defaultInterval).Should(BeNil())
		})

		It("The PipelineRun should exist and succeed", func() {
			Eventually(func() bool {
				pipelineRun, err := framework.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseName, devNamespace)

				if pipelineRun == nil || err != nil {
					return false
				}

				return pipelineRun.HasStarted() && pipelineRun.IsDone() && pipelineRun.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
			}, avgPipelineCompletionTime, defaultInterval).Should(BeTrue())
		})

		It("The Release should have succeeded", func() {
			Eventually(func() bool {
				release, err := framework.ReleaseController.GetRelease(releaseName, devNamespace)

				if err != nil {
					return false
				}

				return release.IsDone() && meta.IsStatusConditionTrue(release.Status.Conditions, "Succeeded")
			}, avgPipelineCompletionTime, defaultInterval).Should(BeTrue())
		})

		It("The Release should reference the release PipelineRun", func() {
			var pipelineRun *v1beta1.PipelineRun

			Eventually(func() bool {
				pipelineRun, err = framework.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseName, devNamespace)

				return pipelineRun != nil && err == nil
			}, avgPipelineCompletionTime, defaultInterval).Should(BeTrue())

			release, err := framework.ReleaseController.GetRelease(releaseName, devNamespace)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(release.Status.ReleasePipelineRun).Should(Equal(fmt.Sprintf("%s/%s", pipelineRun.Namespace, pipelineRun.Name)))
		})
	})
})
