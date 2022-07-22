package build

import (
	. "github.com/onsi/ginkgo/v2"
	//. "github.com/onsi/gomega"

	//"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	//"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	//"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	//"k8s.io/apimachinery/pkg/api/errors"
	//"k8s.io/apimachinery/pkg/runtime"
	//"k8s.io/apimachinery/pkg/runtime/serializer"
	//utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	//klog "k8s.io/klog/v2"
	//"knative.dev/pkg/apis"
)

var _ = framework.JVMBuildSuiteDescribe("JVM Build Service E2E tests", func() {
	defer GinkgoRecover()

	//var appStudioE2EApplicationsNamespace, prGeneratedName string
	//var timeout, interval time.Duration

	//var streamTektonYaml = func(url string) []byte {
	//	resp, err := http.Get(url)
	//	Expect(err).NotTo(HaveOccurred(), "error getting %s", url)
	//	defer resp.Body.Close()
	//	bytes, err := io.ReadAll(resp.Body)
	//	Expect(err).NotTo(HaveOccurred(), "error getting bytes")
	//	return bytes
	//}

	//f, err := framework.NewFramework()
	//Expect(err).NotTo(HaveOccurred())

	// got panics in DeferCleanup when I tried to do multi param invocations, so following the pattern we used in openshift/origin
	AfterAll(func() {
		//if CurrentSpecReport().Failed() {
		//	logs, err := f.TektonController.GetPipelineRunLogs(prGeneratedName, appStudioE2EApplicationsNamespace)
		//	if err != nil {
		//		klog.Infof("got error fetching PR logs: %s", err.Error())
		//	}
		//	klog.Infof("failed PR logs: %s", logs)
		//	trList, err := f.TektonController.ListAllTaskRuns(appStudioE2EApplicationsNamespace)
		//	if err != nil {
		//		klog.Infof("got error fetching TR list: %s", err.Error())
		//	}
		//	for _, tr := range trList.Items {
		//		trLog, err := f.TektonController.GetTaskRunLogs(tr.Name, tr.Namespace)
		//		if err != nil {
		//			klog.Infof("got error fetcing TR logs for %s: %s", tr.Name, err.Error())
		//		}
		//		klog.Infof("task run log for %s: %s", tr.Name, trLog)
		//	}
		//}
		//abList, err := f.JvmbuildserviceController.ListArtifactBuilds(appStudioE2EApplicationsNamespace)
		//if err != nil {
		//	klog.Infof("got error fetching artifactbuilds: %s", err.Error())
		//}
		//for _, ab := range abList.Items {
		//	err := f.JvmbuildserviceController.DeleteArtifactBuild(ab.Name, ab.Namespace)
		//	if err != nil {
		//		klog.Infof("got error deleting AB %s: %s", ab.Name, err.Error())
		//	}
		//}
		//dbList, err := f.JvmbuildserviceController.ListDependencyBuilds(appStudioE2EApplicationsNamespace)
		//if err != nil {
		//	klog.Infof("got error fetching dependencybuilds: %s", err.Error())
		//}
		//for _, db := range dbList.Items {
		//	err := f.JvmbuildserviceController.DeleteDependencyBuild(db.Name, db.Namespace)
		//	if err != nil {
		//		klog.Infof("got error deleting DB %s: %s", db.Name, err.Error())
		//	}
		//}
		//err = f.TektonController.DeletePipelineRun(prGeneratedName, appStudioE2EApplicationsNamespace)
		//if err != nil {
		//	klog.Infof("error deleting pr %s: %s", prGeneratedName, err.Error())
		//}
		//err = f.TektonController.DeletePipeline("sample-component-build", appStudioE2EApplicationsNamespace)
		//if err != nil {
		//	klog.Infof("error deleting pipeline sample-component-build: %s", err.Error())
		//}
		//err = f.TektonController.DeleteTask("maven", appStudioE2EApplicationsNamespace)
		//if err != nil {
		//	klog.Infof("error deleting task maven: %s", err.Error())
		//}
		//err = f.TektonController.DeleteTask("git-clone", appStudioE2EApplicationsNamespace)
		//if err != nil {
		//	klog.Infof("error deleting task git-clone", err.Error())
		//}
	})

	BeforeAll(func() {
		//appStudioE2EApplicationsNamespace = utils.GetEnv(constants.E2E_APPLICATIONS_NAMESPACE_ENV, "appstudio-e2e-test")
		//
		//klog.Infof("Test namespace: %s", appStudioE2EApplicationsNamespace)
		//
		//_, err := f.CommonController.CreateTestNamespace(appStudioE2EApplicationsNamespace)
		//Expect(err).NotTo(HaveOccurred(), "Error when creating/updating '%s' namespace: %v", appStudioE2EApplicationsNamespace, err)
		//
		//timeout = time.Minute * 10
		//interval = time.Second * 10
		//
		//decodingScheme := runtime.NewScheme()
		//utilruntime.Must(v1beta1.AddToScheme(decodingScheme))
		//decoderCodecFactory := serializer.NewCodecFactory(decodingScheme)
		//decoder := decoderCodecFactory.UniversalDecoder(v1beta1.SchemeGroupVersion)
		//
		//gitCloneTaskBytes := streamTektonYaml("https://raw.githubusercontent.com/redhat-appstudio/build-definitions/main/tasks/git-clone.yaml")
		//gitClone := &v1beta1.Task{}
		//err = runtime.DecodeInto(decoder, gitCloneTaskBytes, gitClone)
		//Expect(err).NotTo(HaveOccurred(), "error converting git clone yaml to task obj")
		//err = f.TektonController.DeleteTask(gitClone.Name, appStudioE2EApplicationsNamespace)
		//if err != nil && !errors.IsNotFound(err) {
		//	Expect(err).NotTo(HaveOccurred(), "error deleting git-clone task")
		//}
		//_, err = f.TektonController.CreateTask(gitClone, appStudioE2EApplicationsNamespace)
		//Expect(err).NotTo(HaveOccurred(), "error creating git clone task")
		//
		//mavenLocation := "https://raw.githubusercontent.com/redhat-appstudio/jvm-build-service/main/deploy/base/maven-v0.2.yaml"
		//prOwner := os.Getenv("JVM_BUILD_SERVICE_PR_OWNER")
		//prCommit := os.Getenv("JVM_BUILD_SERVICE_PR_SHA")
		//if len(prOwner) > 0 && len(prCommit) > 0 {
		//	mavenLocation = fmt.Sprintf("https://raw.githubusercontent.com/%s/jvm-build-service/%s/deploy/base/maven-v0.2.yaml", prOwner, prCommit)
		//}
		//klog.Infof("fetching maven def from %s", mavenLocation)
		//mavenData := streamTektonYaml(mavenLocation)
		//maven := &v1beta1.Task{}
		//err = runtime.DecodeInto(decoder, mavenData, maven)
		//Expect(err).NotTo(HaveOccurred(), "error creating maven task")
		//// override images if needed
		//analyserImage := os.Getenv("JVM_BUILD_SERVICE_ANALYZER_IMAGE")
		//if len(analyserImage) > 0 {
		//	klog.Infof("PR analyzer image: %s", analyserImage)
		//	for _, step := range maven.Spec.Steps {
		//		if step.Name != "analyse-dependencies" {
		//			continue
		//		}
		//		klog.Infof("Updating analyse-dependencies step with image %s", analyserImage)
		//		step.Image = analyserImage
		//	}
		//}
		//sidecarImage := os.Getenv("JVM_BUILD_SERVICE_SIDECAR_IMAGE")
		//if len(sidecarImage) > 0 {
		//	klog.Infof("PR sidecar image: %s", sidecarImage)
		//	for _, sidecar := range maven.Spec.Sidecars {
		//		if sidecar.Name != "proxy" {
		//			continue
		//		}
		//		klog.Infof("Updating proxy sidecar with image %s", sidecarImage)
		//		sidecar.Image = sidecarImage
		//	}
		//}
		//
		//err = f.TektonController.DeleteTask("maven", appStudioE2EApplicationsNamespace)
		//if err != nil && !errors.IsNotFound(err) {
		//	Expect(err).NotTo(HaveOccurred(), "error cleaning up maven task before create")
		//}
		//_, err = f.TektonController.CreateTask(maven, appStudioE2EApplicationsNamespace)
		//Expect(err).NotTo(HaveOccurred(), "error creating maven task")
		//
		//pipelineLocation := "https://raw.githubusercontent.com/redhat-appstudio/jvm-build-service/main/hack/examples/pipeline.yaml"
		//if len(prOwner) > 0 && len(prCommit) > 0 {
		//	pipelineLocation = fmt.Sprintf("https://raw.githubusercontent.com/%s/jvm-build-service/%s/hack/examples/pipeline.yaml", prOwner, prCommit)
		//}
		//klog.Infof("fetching pipeline def from %s", pipelineLocation)
		//pipelineData := streamTektonYaml(pipelineLocation)
		//pipeline := &v1beta1.Pipeline{}
		//err = runtime.DecodeInto(decoder, pipelineData, pipeline)
		//Expect(err).NotTo(HaveOccurred(), "error decoding pipeline")
		//
		//err = f.TektonController.DeletePipeline(pipeline.Name, appStudioE2EApplicationsNamespace)
		//if err != nil && !errors.IsNotFound(err) {
		//	Expect(err).NotTo(HaveOccurred(), "error cleaning up build pipeline before create")
		//}
		//_, err = f.TektonController.CreatePipeline(pipeline, appStudioE2EApplicationsNamespace)
		//Expect(err).NotTo(HaveOccurred(), "error creating build pipeline")
		//
		//runLocation := "https://raw.githubusercontent.com/redhat-appstudio/jvm-build-service/main/hack/examples/run.yaml"
		//if len(prOwner) > 0 && len(prCommit) > 0 {
		//	runLocation = fmt.Sprintf("https://raw.githubusercontent.com/%s/jvm-build-service/%s/hack/examples/run.yaml", prOwner, prCommit)
		//}
		//klog.Infof("fetching pipelinerun def from %s", runLocation)
		//runData := streamTektonYaml(runLocation)
		//run := &v1beta1.PipelineRun{}
		//err = runtime.DecodeInto(decoder, runData, run)
		//Expect(err).NotTo(HaveOccurred(), "error decoding build pipelinerun")
		//
		//// since we use generated name no need to cleanup before create
		//run, err = f.TektonController.CreatePipelineRun(run, appStudioE2EApplicationsNamespace)
		//Expect(err).NotTo(HaveOccurred(), "error creating build pipelinerun")
		//prGeneratedName = run.Name
		//
		//klog.Infof("Generated pipeline run %s", prGeneratedName)
	})

	When("pipelinerun with the appropriate jvm-build-service repository analysis are launched", func() {

		//It("those pipeline runs complete successfully", func() {
		//	Eventually(func() bool {
		//		pr, err := f.TektonController.GetPipelineRun(prGeneratedName, appStudioE2EApplicationsNamespace)
		//		if err != nil {
		//			klog.Infof("get of pr %s returned error: %s", prGeneratedName, err.Error())
		//			return false
		//		}
		//		if !pr.IsDone() {
		//			klog.Infof("pipeline run %s not done", pr.Name)
		//			return false
		//		}
		//		if !pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsTrue() {
		//			// just because the condition succeeded is not set does not mean it won't be soon
		//			prBytes, err := json.MarshalIndent(pr, "", "  ")
		//			if err != nil {
		//				klog.Infof("problem marshalling failed pipelinerun to bytes: %s", err.Error())
		//				return false
		//			}
		//			klog.Infof("not yet successful pipeline run: %s", string(prBytes))
		//			return false
		//		}
		//		return true
		//	}, timeout, interval).Should(BeTrue(), "timed out when waiting for the pipeline run to complete")
		//})
		// Blocked by https://issues.redhat.com/browse/PLNSRVCE-519
		// It("that artifactbuilds and dependencybuilds are generated", func() {
		// 	Eventually(func() bool {
		// 		abList, err := f.JvmbuildserviceController.ListArtifactBuilds(appStudioE2EApplicationsNamespace)
		// 		if err != nil {
		// 			klog.Infof("error listing artifactbuilds: %s", err.Error())
		// 			return false
		// 		}
		// 		gotABs := false
		// 		if len(abList.Items) > 0 {
		// 			gotABs = true
		// 		}
		// 		dbList, err := f.JvmbuildserviceController.ListDependencyBuilds(appStudioE2EApplicationsNamespace)
		// 		if err != nil {
		// 			klog.Infof("error listing dependencybuilds: %s", err.Error())
		// 			return false
		// 		}
		// 		gotDBs := false
		// 		if len(dbList.Items) > 0 {
		// 			gotDBs = true
		// 		}
		// 		if gotABs && gotDBs {
		// 			return true
		// 		}
		// 		return false
		// 	}, timeout, interval).Should(BeTrue(), "timed out when waiting for the generation of artifactbuilds and dependencybuilds")
		// })

		// It("that pipelinerun generates task runs related to artifactbuilds and dependencybuilds", func() {
		// 	Eventually(func() bool {
		// 		// as the values of the jvm build service labels are volatile, we'll just fetch all of them
		// 		// and validate label keys
		// 		taskRuns, err := f.TektonController.ListAllTaskRuns(appStudioE2EApplicationsNamespace)
		// 		if err != nil {
		// 			klog.Infof("list on label taskruns returned error: %s", err.Error())
		// 			return false
		// 		}
		// 		if len(taskRuns.Items) == 0 {
		// 			klog.Infof("list 0 length")
		// 			return false
		// 		}
		// 		foundGenericLabel := false
		// 		foundABRLabel := false
		// 		foundDBLabel := false
		// 		for _, taskRun := range taskRuns.Items {
		// 			klog.Infof("taskrun %s has label map of len %d", taskRun.Name, len(taskRun.Labels))
		// 			for k := range taskRun.Labels {
		// 				klog.Infof("taskrun %s has label %s", taskRun.Name, k)
		// 				if k == "jvmbuildservice.io/taskrun" {
		// 					foundGenericLabel = true
		// 				}
		// 				if k == "jvmbuildservice.io/abr-id" {
		// 					foundABRLabel = true
		// 				}
		// 				if k == "jvmbuildservice.io/dependencybuild-id" {
		// 					foundDBLabel = true
		// 				}
		// 				if foundGenericLabel && foundABRLabel && foundDBLabel {
		// 					return true
		// 				}
		// 			}
		// 		}

		// 		return false
		// 	}, timeout, interval).Should(BeTrue(), "timed out waiting for artifactbuild/dependencybuild tekton objexts")
		// })
		// It("that some artifactbuilds and dependencybuilds complete", func() {
		// 	Eventually(func() bool {
		// 		abList, err := f.JvmbuildserviceController.ListArtifactBuilds(appStudioE2EApplicationsNamespace)
		// 		if err != nil {
		// 			klog.Infof("error listing artifactbuilds: %s", err.Error())
		// 			return false
		// 		}
		// 		abComplete := false
		// 		for _, ab := range abList.Items {
		// 			if ab.Status.State == v1alpha1.ArtifactBuildStateComplete {
		// 				abComplete = true
		// 				break
		// 			}
		// 		}
		// 		dbList, err := f.JvmbuildserviceController.ListDependencyBuilds(appStudioE2EApplicationsNamespace)
		// 		if err != nil {
		// 			klog.Infof("error listing dependencybuilds: %s", err.Error())
		// 			return false
		// 		}
		// 		dbComplete := false
		// 		for _, db := range dbList.Items {
		// 			if db.Status.State == v1alpha1.DependencyBuildStateComplete {
		// 				dbComplete = true
		// 				break
		// 			}
		// 		}
		// 		if abComplete && dbComplete {
		// 			return true
		// 		}
		// 		return false
		// 	}, 2*timeout, interval).Should(BeTrue(), "timed out waiting for some artifactbuilds/dependencybuilds to complete")
		// })
	})
})
