package service

import (
	"fmt"

	tektonutils "github.com/redhat-appstudio/release-service/tekton/utils"
	"k8s.io/apimachinery/pkg/api/meta"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	releasecommon "github.com/redhat-appstudio/e2e-tests/tests/release"
	releaseApi "github.com/redhat-appstudio/release-service/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = framework.ReleaseServiceSuiteDescribe("ReleasePlan and ReleasePlanAdmission match", Label("release-service", "release_plan_and_admission", "HACBS"), func() {
	defer GinkgoRecover()

	var fw *framework.Framework
	var err error
	var devNamespace string
	var managedNamespace = utils.GetGeneratedNamespace("plan-and-admission-managed")

	var releasePlanCR, secondReleasePlanCR *releaseApi.ReleasePlan
	var releasePlanAdmissionCR *releaseApi.ReleasePlanAdmission
	AfterEach(framework.ReportFailure(&fw))

	BeforeAll(func() {
		// Initialize the tests controllers
		fw, err = framework.NewFramework(utils.GetGeneratedNamespace("rel-plan-admis"))
		Expect(err).NotTo(HaveOccurred())
		devNamespace = fw.UserNamespace

		_, err = fw.AsKubeAdmin.CommonController.CreateTestNamespace(managedNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating managedNamespace: %v", err)

		_, err = fw.AsKubeAdmin.HasController.CreateApplication(releasecommon.ApplicationNameDefault, devNamespace)
		Expect(err).NotTo(HaveOccurred())

		//Create ReleasePlan
		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(releasecommon.SourceReleasePlanName, devNamespace, releasecommon.ApplicationNameDefault, managedNamespace, "true", nil)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if !CurrentSpecReport().Failed() {
			Expect(fw.AsKubeAdmin.CommonController.DeleteNamespace(managedNamespace)).NotTo(HaveOccurred())
			Expect(fw.SandboxController.DeleteUserSignup(fw.UserName)).To(BeTrue())
		}
	})

	var _ = Describe("RP and PRA status change verification", func() {
		It("verifies that the ReleasePlan CR is unmatched in the beginning", func() {
			var condition *metav1.Condition
			Eventually(func() error {
				releasePlanCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlan(releasecommon.SourceReleasePlanName, devNamespace)
				Expect(err).NotTo(HaveOccurred())
				condition = meta.FindStatusCondition(releasePlanCR.Status.Conditions, releaseApi.MatchedConditionType.String())
				if condition == nil {
					return fmt.Errorf("the MatchedConditon of %s is still not set", releasePlanCR.Name)
				}
				return nil
			}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed())
			condition = meta.FindStatusCondition(releasePlanCR.Status.Conditions, releaseApi.MatchedConditionType.String())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		})

		It("Creates ReleasePlanAdmission CR in corresponding managed namespace", func() {
			_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace, "", devNamespace, releasecommon.ReleaseStrategyPolicyDefault, releasecommon.ReleasePipelineServiceAccountDefault, []string{releasecommon.ApplicationNameDefault}, true, &tektonutils.PipelineRef{
				Resolver: "git",
				Params: []tektonutils.Param{
					{Name: "url", Value: releasecommon.RelSvcCatalogURL},
					{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
					{Name: "pathInRepo", Value: "pipelines/e2e/e2e.yaml"},
				},
			}, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		When("ReleasePlanAdmission CR is created in managed namespace", func() {
			It("verifies that the ReleasePlan CR is set to matched", func() {
				var condition *metav1.Condition
				Eventually(func() error {
					releasePlanCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlan(releasecommon.SourceReleasePlanName, devNamespace)
					Expect(err).NotTo(HaveOccurred())
					condition = meta.FindStatusCondition(releasePlanCR.Status.Conditions, releaseApi.MatchedConditionType.String())
					if condition == nil {
						return fmt.Errorf("the MatchedConditon of %s is still not set", releasePlanCR.Name)
					}
					// it may need a period of time for the ReleasePlanCR to be reconciled
					if condition.Status == metav1.ConditionFalse {
						return fmt.Errorf("the MatchedConditon of %s has not reconciled yet", releasePlanCR.Name)
					}
					return nil
				}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(releasePlanCR.Status.ReleasePlanAdmission.Name).To(Equal(managedNamespace + "/" + releasecommon.TargetReleasePlanAdmissionName))
				Expect(releasePlanCR.Status.ReleasePlanAdmission.Active).To(BeTrue())
			})

			It("verifies that the ReleasePlanAdmission CR is set to matched", func() {
				var condition *metav1.Condition
				Eventually(func() error {
					releasePlanAdmissionCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace)
					Expect(err).NotTo(HaveOccurred())
					condition = meta.FindStatusCondition(releasePlanAdmissionCR.Status.Conditions, releaseApi.MatchedConditionType.String())
					if condition.Status == metav1.ConditionFalse {
						return fmt.Errorf("the MatchedConditon of %s has not reconciled yet", releasePlanCR.Name)
					}
					return nil
				}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed(), "time out when waiting for ReleasePlanAdmission being reconciled to matched")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(releasePlanAdmissionCR.Status.ReleasePlans).To(HaveLen(1))
				Expect(releasePlanAdmissionCR.Status.ReleasePlans).To(Equal([]releaseApi.MatchedReleasePlan{{Name: devNamespace + "/" + releasecommon.SourceReleasePlanName, Active: true}}))
			})
		})

		It("Creates a manual release ReleasePlan CR in devNamespace", func() {
			_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(releasecommon.SecondReleasePlanName, devNamespace, releasecommon.ApplicationNameDefault, managedNamespace, "false", nil)
			Expect(err).NotTo(HaveOccurred())
		})

		When("the second ReleasePlan CR is created", func() {
			It("verifies that the second ReleasePlan CR is set to matched", func() {
				var condition *metav1.Condition
				Eventually(func() error {
					secondReleasePlanCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlan(releasecommon.SecondReleasePlanName, devNamespace)
					Expect(err).NotTo(HaveOccurred())
					condition = meta.FindStatusCondition(secondReleasePlanCR.Status.Conditions, releaseApi.MatchedConditionType.String())

					if condition == nil {
						return fmt.Errorf("the MatchedConditon of %s is still not set", secondReleasePlanCR.Name)
					}
					// it may need a period of time for the secondReleasePlanCR to be reconciled
					if condition.Status == metav1.ConditionFalse {
						return fmt.Errorf("the MatchedConditon of %s has not reconciled yet", secondReleasePlanCR.Name)
					}
					return nil
				}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(secondReleasePlanCR.Status.ReleasePlanAdmission.Name).To(Equal(managedNamespace + "/" + releasecommon.TargetReleasePlanAdmissionName))
				Expect(secondReleasePlanCR.Status.ReleasePlanAdmission.Active).To(BeTrue())
			})

			It("verifies that the ReleasePlanAdmission CR has two matched ReleasePlan CRs", func() {
				Eventually(func() error {
					releasePlanAdmissionCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace)
					Expect(err).NotTo(HaveOccurred())
					condition := meta.FindStatusCondition(releasePlanAdmissionCR.Status.Conditions, releaseApi.MatchedConditionType.String())
					if condition == nil {
						return fmt.Errorf("failed to get the MatchedConditon of RPA %s ", releasePlanAdmissionCR.Name)
					}

					if len(releasePlanAdmissionCR.Status.ReleasePlans) < 2 {
						return fmt.Errorf("the second ReleasePlan CR has not being added to %s", releasePlanAdmissionCR.Name)
					}
					Expect(condition).NotTo(BeNil())
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					return nil
				}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed(), fmt.Sprintf("time out when waiting for ReleasePlanAdmission %s being reconciled to matched", releasePlanAdmissionCR.Name))
				Expect(releasePlanAdmissionCR.Status.ReleasePlans).To(HaveLen(2))
				Expect(releasePlanAdmissionCR.Status.ReleasePlans).To(Equal([]releaseApi.MatchedReleasePlan{{Name: devNamespace + "/" + releasecommon.SourceReleasePlanName, Active: true}, {Name: devNamespace + "/" + releasecommon.SecondReleasePlanName, Active: false}}))
			})
		})

		It("deletes one ReleasePlan CR", func() {
			err = fw.AsKubeAdmin.ReleaseController.DeleteReleasePlan(releasecommon.SourceReleasePlanName, devNamespace, true)
			Expect(err).NotTo(HaveOccurred())
		})

		When("One ReleasePlan CR is deleted in managed namespace", func() {
			It("verifies that the ReleasePlanAdmission CR has only one matching ReleasePlan", func() {
				Eventually(func() error {
					releasePlanAdmissionCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace)
					Expect(err).NotTo(HaveOccurred())
					condition := meta.FindStatusCondition(releasePlanAdmissionCR.Status.Conditions, releaseApi.MatchedConditionType.String())
					if condition == nil {
						return fmt.Errorf("failed to find the MatchedConditon of %s", releasePlanAdmissionCR.Name)
					}

					if len(releasePlanAdmissionCR.Status.ReleasePlans) > 1 {
						return fmt.Errorf("ReleasePlan CR is deleted, but ReleasePlanAdmission CR %s has not been reconciled", releasePlanAdmissionCR.Name)
					}
					Expect(condition).NotTo(BeNil())
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					return nil
				}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed(), fmt.Sprintf("time out when waiting for ReleasePlanAdmission %s being reconciled after one ReleasePlan is deleted", releasePlanAdmissionCR.Name))
				Expect(releasePlanAdmissionCR.Status.ReleasePlans).To(HaveLen(1))
				Expect(releasePlanAdmissionCR.Status.ReleasePlans).To(Equal([]releaseApi.MatchedReleasePlan{{Name: devNamespace + "/" + releasecommon.SecondReleasePlanName, Active: false}}))
			})
		})

		It("deletes the ReleasePlanAdmission CR", func() {
			err = fw.AsKubeAdmin.ReleaseController.DeleteReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace, true)
			Expect(err).NotTo(HaveOccurred())
		})

		When("ReleasePlanAdmission CR is deleted in managed namespace", func() {
			It("verifies that the ReleasePlan CR has no matched ReleasePlanAdmission", func() {
				Eventually(func() error {
					secondReleasePlanCR, err = fw.AsKubeAdmin.ReleaseController.GetReleasePlan(releasecommon.SecondReleasePlanName, devNamespace)
					Expect(err).NotTo(HaveOccurred())
					condition := meta.FindStatusCondition(secondReleasePlanCR.Status.Conditions, releaseApi.MatchedConditionType.String())
					if condition == nil {
						return fmt.Errorf("failed to get the MatchedConditon of %s", secondReleasePlanCR.Name)
					}

					// it may need a period of time for the secondReleasePlanCR to be reconciled
					if condition.Status == metav1.ConditionTrue {
						return fmt.Errorf("the MatchedConditon of %s has not reconciled yet", secondReleasePlanCR.Name)
					}
					return nil
				}, releasecommon.ReleasePlanStatusUpdateTimeout, releasecommon.DefaultInterval).Should(Succeed())
				Expect(secondReleasePlanCR.Status.ReleasePlanAdmission).To(Equal(releaseApi.MatchedReleasePlanAdmission{}))
			})
		})
	})
})
