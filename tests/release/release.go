package release

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	gitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/appstudio-shared/apis/appstudio.redhat.com/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	klog "k8s.io/klog/v2"
	"knative.dev/pkg/apis"
)

var snapshotComponents = []gitopsv1alpha1.ApplicationSnapshotComponent{
	{"component-1", "quay.io/redhat-appstudio/component1@sha256:d5e85e49c89df42b221d972f5b96c6507a8124717a6e42e83fd3caae1031d514"},
	{"component-2", "quay.io/redhat-appstudio/component2@sha256:a01dfd18cf8ca8b68770b09a9b6af0fd7c6d1f8644c7ab97f0e06c34dfc5860e"},
	{"component-3", "quay.io/redhat-appstudio/component3@sha256:d90a0a33e4c5a1daf5877f8dd989a570bfae4f94211a8143599245e503775b1f"},
}

var _ = framework.ReleaseSuiteDescribe("test-release-service-happy-path", Label("release"), func() {
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

		// Wait until the "pipeline" SA is created and ready with secrets by the openshift-pipelines operator
		klog.Infof("Wait until the 'pipeline' SA is created in %s namespace \n", managedNamespace)
		Eventually(func() bool {
			sa, err := framework.CommonController.GetServiceAccount("pipeline", managedNamespace)
			return sa != nil && err == nil
		}, 1*time.Minute, defaultInterval).Should(BeTrue(), "timed out when waiting for the \"pipeline\" SA to be created")
	})

	AfterAll(func() {
		// Delete the dev and managed namespaces with all the resources created in them
		Expect(framework.CommonController.DeleteNamespace(devNamespace)).NotTo(HaveOccurred())
		Expect(framework.CommonController.DeleteNamespace(managedNamespace)).NotTo(HaveOccurred())
	})

	var _ = Describe("Creation of the 'Happy path' resources", func() {
		It("Create an ApplicationSnapshot.", func() {
			_, err := framework.ReleaseController.CreateApplicationSnapshot(snapshotName, devNamespace, applicationName, snapshotComponents)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create Release Strategy", func() {
			_, err := framework.ReleaseController.CreateReleaseStrategy(releaseStrategyName, managedNamespace, releasePipelineName, releasePipelineBundle, releaseStrategyPolicy)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create ReleaseLink in dev namespace", func() {
			_, err := framework.ReleaseController.CreateReleaseLink(sourceReleaseLinkName, devNamespace, applicationName, managedNamespace, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create ReleaseLink in managed namespace", func() {
			_, err := framework.ReleaseController.CreateReleaseLink(targetReleaseLinkName, managedNamespace, applicationName, devNamespace, releaseStrategyName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create a Release", func() {
			_, err := framework.ReleaseController.CreateRelease(releaseName, devNamespace, snapshotName, sourceReleaseLinkName)
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
