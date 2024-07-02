package build

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	"github.com/konflux-ci/e2e-tests/pkg/utils/build"
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"

	"github.com/devfile/library/v2/pkg/util"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"gopkg.in/yaml.v3"
)

const (
	Ec2ArmTag              = "multi-platform-e2e-arm64"
	HostConfig             = "host-config"
	ControllerNamespace    = "multi-platform-controller"
	AwsSecretName          = "awskeys"
	IbmSecretName          = "ibmkey"
	IbmKey                 = "multi-platform-tests"
	SshSecretName          = "sshkeys"
	Ec2User                = "ec2-user"
	AwsRegion              = "us-east-1"
	AwsPlatform            = "linux/arm64"
	DynamicMaxInstances    = "1"
	IbmZUrl                = "https://us-east.iaas.cloud.ibm.com/v1"
	IbmPUrl                = "https://us-south.power-iaas.cloud.ibm.com"
	CRN                    = "crn:v1:bluemix:public:power-iaas:dal10:a/934e118c399b4a28a70afdf2210d708f:8c9ef568-16a5-4aa2-bfd5-946349c9aeac::"
	MultiPlatformSecretKey = "build.appstudio.redhat.com/multi-platform-secret"
	MultiPlatformConfigKey = "build.appstudio.redhat.com/multi-platform-config"
)

var (
	IbmVpc                       = "us-east-default-vpc"
	multiPlatformProjectGitUrl   = utils.GetEnv("MULTI_PLATFORM_TEST_REPO_URL", "https://github.com/devfile-samples/devfile-sample-go-basic")
	multiPlatformProjectRevision = utils.GetEnv("MULTI_PLATFORM_TEST_REPO_REVISION", "c713067b0e65fb3de50d1f7c457eb51c2ab0dbb0")
	timeout                      = 20 * time.Minute
	interval                     = 10 * time.Second
)

type RPMLockFile struct {
	LockFileVersion int    `yaml:"lockfileVersion"`
	LockFileVendor  string `yaml:"lockfileVendor"`
	Arches          []struct {
		Arch     string `yaml:"arch"`
		Packages []struct {
			Url       string `yaml:"url"`
			Repoid    string `yaml:"repoid"`
			Size      int    `yaml:"size"`
			Checksum  string `yaml:"checksum"`
			Name      string `yaml:"name"`
			EVR       string `yaml:"evr"`
			SourceRPM string `yaml:"sourcerpm"`
		}
		Source []struct {
			Url      string `yaml:"url"`
			Repoid   string `yaml:"repoid"`
			Size     int    `yaml:"size"`
			Checksum string `yaml:"checksum"`
			Name     string `yaml:"name"`
			EVR      string `yaml:"evr"`
		}
	}
}

var _ = framework.MultiPlatformBuildSuiteDescribe("Multi Platform Controller E2E tests", Pending, Label("multi-platform"), func() {
	var f *framework.Framework
	AfterEach(framework.ReportFailure(&f))
	var err error

	defer GinkgoRecover()

	Describe("aws host-pool allocation", Label("aws-host-pool"), func() {

		var testNamespace, applicationName, componentName, multiPlatformSecretName, host, userDir string
		var component *appservice.Component

		// TODO: move this repo into redhat-appstudio-qe Github organization
		const testRepoUrl = "https://github.com/cqi-stonesoup-test/multi-arch-builds"

		AfterAll(func() {
			// Cleanup aws secet and host-config
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, AwsSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteConfigMap(HostConfig, ControllerNamespace, true)).To(Succeed())

			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
			} else {
				Expect(f.AsKubeAdmin.CommonController.StoreAllPods(testNamespace)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.StoreAllPipelineRuns(testNamespace)).To(Succeed())
			}
		})

		BeforeAll(func() {

			f, err = framework.NewFramework(utils.GetGeneratedNamespace("multi-platform-host"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace
			Expect(testNamespace).NotTo(BeNil(), "failed to create sandbox user namespace")

			Expect(err).ShouldNot(HaveOccurred())

			err = createConfigMapForHostPool(f)
			Expect(err).ShouldNot(HaveOccurred())

			err = createSecretForHostPool(f)
			Expect(err).ShouldNot(HaveOccurred())

			component, applicationName, componentName = createApplicationAndComponent(f, testNamespace, "ARM64", testRepoUrl, "main")
		})

		When("the Component with multi-platform-build is created", func() {
			It("a PipelineRun is triggered", func() {
				validatePipelineRunIsRunning(f, componentName, applicationName, testNamespace)
			})

			It("the build-container task from component pipelinerun is buildah-remote", func() {
				_, multiPlatformSecretName = validateBuildContainerTaskIsBuildahRemote(f, componentName, applicationName, testNamespace)
			})
			It("The multi platform secret is populated", func() {
				var secret *v1.Secret
				Eventually(func() error {
					secret, err = f.AsKubeAdmin.CommonController.GetSecret(testNamespace, multiPlatformSecretName)
					if err != nil {
						return err
					}
					return nil
				}, timeout, interval).Should(Succeed(), "timed out when verifying the secret is created")

				// Get the host and the user directory so we can verify they are cleaned up at the end of the run
				fullHost, present := secret.Data["host"]
				Expect(present).To(BeTrue())

				userDirTmp, present := secret.Data["user-dir"]
				Expect(present).To(BeTrue())
				userDir = string(userDirTmp)
				hostParts := strings.Split(string(fullHost), "@")
				host = strings.TrimSpace(hostParts[1])
			})

			It("that PipelineRun completes successfully", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "", f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
			})

			It("test that cleanup happened successfully", func() {

				// Parse the private key
				signer, err := ssh.ParsePrivateKey([]byte(os.Getenv("MULTI_PLATFORM_AWS_SSH_KEY")))
				if err != nil {
					log.Fatalf("Unable to parse private key: %v", err)
				}
				// SSH configuration using public key authentication
				config := &ssh.ClientConfig{
					User: Ec2User,
					Auth: []ssh.AuthMethod{
						ssh.PublicKeys(signer),
					},
					HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec
				}
				Eventually(func() error {
					client, err := ssh.Dial("tcp", host+":22", config)
					if err != nil {
						return err
					}
					defer client.Close()

					// Create a new session
					session, err := client.NewSession()
					if err != nil {
						return err
					}
					defer session.Close()

					// Check if the file exists
					if dirExists(session, userDir) {
						return fmt.Errorf("cleanup not successful, user dir still exists")
					}
					return nil
				}, timeout, interval).Should(Succeed(), "timed out when verifying that the remote host was cleaned up correctly")
			})

			It("prefetched SRPMs are included", func() {
				pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
				Expect(err).Should(Succeed())
				client := f.AsKubeAdmin.CommonController.KubeRest()
				sourceImageUrl, err := f.AsKubeAdmin.TektonController.GetTaskRunResult(client, pr, "build-source-image", "IMAGE_URL")
				Expect(err).Should(Succeed())

				imageDir, err := build.ExtractImage(sourceImageUrl)
				Expect(err).Should(Succeed())
				defer os.RemoveAll(imageDir)

				entries, err := os.ReadDir(filepath.Join(imageDir, "rpm_dir"))
				Expect(err).Should(Succeed())

				srpmSet := make(map[string]int)
				for _, entry := range entries {
					srpmSet[entry.Name()] = 1
				}

				content, err := build.ReadFileFromGitRepo(testRepoUrl, "rpm.lock.file", "main")
				Expect(err).Should(Succeed())

				rpmLockFile := RPMLockFile{}
				Expect(yaml.Unmarshal([]byte(content), &rpmLockFile)).Should(Succeed())

				// Ideally, this check should be in the CheckSourceImage function. However, it is
				// not easy for multi-platform builds tests here to set hermetic parameter for the
				// triggered PipelineRun, which causes the prefetched sources check for hermetic.
				// So, before figuring out a good solution, make this check separate here.

				for _, arch := range rpmLockFile.Arches {
					for _, sourceInfo := range arch.Source {
						srpmFilename := filepath.Base(sourceInfo.Url)
						if _, exists := srpmSet[srpmFilename]; !exists {
							Fail(fmt.Sprintf("SRPM %s is not included for arch %s", srpmFilename, arch.Arch))
						}
					}
				}
			})
		})
	})

	Describe("aws dynamic allocation", Label("aws-dynamic"), func() {
		var testNamespace, applicationName, componentName, multiPlatformSecretName, multiPlatformTaskName, dynamicInstanceTag, instanceId string
		var component *appservice.Component

		AfterAll(func() {
			// Cleanup aws&ssh secrets and host-config
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, AwsSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, SshSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteConfigMap(HostConfig, ControllerNamespace, true)).To(Succeed())

			//Forcefully remove instance incase it is not removed by multi-platform-controller
			err = terminateAwsInstance(instanceId)
			if err != nil {
				GinkgoWriter.Printf("error terminating instance again: %v", err)
			}

			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
			} else {
				Expect(f.AsKubeAdmin.CommonController.StoreAllPods(testNamespace)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.StoreAllPipelineRuns(testNamespace)).To(Succeed())
			}
		})

		BeforeAll(func() {

			f, err = framework.NewFramework(utils.GetGeneratedNamespace("multi-platform-dynamic"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace
			Expect(testNamespace).NotTo(BeNil(), "failed to create sandbox user namespace")
			Expect(err).ShouldNot(HaveOccurred())

			dynamicInstanceTag := "dynamic-instance-" + util.GenerateRandomString(4)
			GinkgoWriter.Printf("Generated dynamic instance tag: %q\n", dynamicInstanceTag)

			// Restart multi-platform-controller pod to reload configMap again
			restartMultiPlatformControllerPod(f)

			err = createConfigMapForDynamicInstance(f, dynamicInstanceTag)
			Expect(err).ShouldNot(HaveOccurred())

			err = createSecretsForDynamicInstance(f)
			Expect(err).ShouldNot(HaveOccurred())

			component, applicationName, componentName = createApplicationAndComponent(f, testNamespace, "ARM64", "", "")
		})

		When("the Component with multi-platform-build is created", func() {
			It("a PipelineRun is triggered", func() {
				validatePipelineRunIsRunning(f, componentName, applicationName, testNamespace)
			})

			It("the build-container task from component pipelinerun is buildah-remote", func() {
				multiPlatformTaskName, multiPlatformSecretName = validateBuildContainerTaskIsBuildahRemote(f, componentName, applicationName, testNamespace)
			})

			It("The multi platform secret is populated", func() {
				instanceId = validateMultiPlatformSecretIsPopulated(f, testNamespace, multiPlatformTaskName, multiPlatformSecretName)
			})

			It("that PipelineRun completes successfully", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "", f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
			})

			It("check cleanup happened successfully", func() {
				Eventually(func() error {
					instances, err := getDynamicAwsInstance(dynamicInstanceTag)
					if err != nil {
						return err
					}
					if len(instances) != 0 {
						return fmt.Errorf("instance is not cleaned up properly, current running instances: %v", instances)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "timed out when verifying that the remote host was cleaned up correctly")
			})

		})
	})
	// TODO: Enable the test after https://issues.redhat.com/browse/KFLUXBUGS-1179 is fixed
	Describe("ibm system z dynamic allocation", Label("ibmz-dynamic"), Pending, func() {
		var testNamespace, applicationName, componentName, multiPlatformSecretName, multiPlatformTaskName, dynamicInstanceTag, instanceId string
		var component *appservice.Component

		AfterAll(func() {
			//Cleanup ibm&ssh secrets and host-config
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, IbmSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, SshSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteConfigMap(HostConfig, ControllerNamespace, true)).To(Succeed())

			//Forcefully remove instance incase it is not removed by multi-platform-controller
			err = terminateIbmZInstance(instanceId)
			if err != nil {
				GinkgoWriter.Printf("error terminating instance again: %v", err)
			}

			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
			} else {
				Expect(f.AsKubeAdmin.CommonController.StoreAllPods(testNamespace)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.StoreAllPipelineRuns(testNamespace)).To(Succeed())
			}
		})

		BeforeAll(func() {

			f, err = framework.NewFramework(utils.GetGeneratedNamespace("multi-platform-ibmz"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace
			Expect(testNamespace).NotTo(BeNil(), "failed to create sandbox user namespace")
			Expect(err).ShouldNot(HaveOccurred())

			restartMultiPlatformControllerPod(f)

			dynamicInstanceTag = "ibmz-instance-" + util.GenerateRandomString(4)
			err = createConfigMapForIbmZDynamicInstance(f, dynamicInstanceTag)
			Expect(err).ShouldNot(HaveOccurred())

			err = createSecretsForIbmDynamicInstance(f)
			Expect(err).ShouldNot(HaveOccurred())

			component, applicationName, componentName = createApplicationAndComponent(f, testNamespace, "S390X", "", "")
		})

		When("the Component with multi-platform-build is created", func() {
			It("a PipelineRun is triggered", func() {
				validatePipelineRunIsRunning(f, componentName, applicationName, testNamespace)
			})

			It("the build-container task from component pipelinerun is buildah-remote", func() {
				multiPlatformTaskName, multiPlatformSecretName = validateBuildContainerTaskIsBuildahRemote(f, componentName, applicationName, testNamespace)
			})

			It("The multi platform secret is populated", func() {
				instanceId = validateMultiPlatformSecretIsPopulated(f, testNamespace, multiPlatformTaskName, multiPlatformSecretName)
			})

			It("that PipelineRun completes successfully", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "", f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
			})

			It("check cleanup happened successfully", func() {
				Eventually(func() error {
					instances, err := getIbmZDynamicInstances(dynamicInstanceTag)
					if err != nil {
						return err
					}
					if len(instances) != 0 {
						return fmt.Errorf("instance is not cleaned up properly, current running instances: %v", instances)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "timed out when verifying that the remote host was cleaned up correctly")
			})

		})
	})
	// TODO: Enable the test after https://issues.redhat.com/browse/KFLUXBUGS-1179 is fixed
	Describe("ibm power pc dynamic allocation", Label("ibmp-dynamic"), Pending, func() {
		var testNamespace, applicationName, componentName, multiPlatformSecretName, multiPlatformTaskName, dynamicInstanceTag, instanceId string
		var component *appservice.Component

		AfterAll(func() {
			// Cleanup ibm key & ssh secrets and host-config
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, IbmSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteSecret(ControllerNamespace, SshSecretName)).To(Succeed())
			Expect(f.AsKubeAdmin.CommonController.DeleteConfigMap(HostConfig, ControllerNamespace, true)).To(Succeed())

			//Forcefully remove instance incase it is not removed by multi-platform-controller
			err = terminateIbmPInstance(instanceId)
			if err != nil {
				GinkgoWriter.Printf("error terminating instance again: %v", err)
			}

			if !CurrentSpecReport().Failed() {
				Expect(f.AsKubeAdmin.HasController.DeleteComponent(componentName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.HasController.DeleteApplication(applicationName, testNamespace, false)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(testNamespace)).To(Succeed())
			} else {
				Expect(f.AsKubeAdmin.CommonController.StoreAllPods(testNamespace)).To(Succeed())
				Expect(f.AsKubeAdmin.TektonController.StoreAllPipelineRuns(testNamespace)).To(Succeed())
			}
		})

		BeforeAll(func() {

			f, err = framework.NewFramework(utils.GetGeneratedNamespace("multi-platform-ibmp"))
			Expect(err).NotTo(HaveOccurred())
			testNamespace = f.UserNamespace
			Expect(testNamespace).NotTo(BeNil(), "failed to create sandbox user namespace")
			Expect(err).ShouldNot(HaveOccurred())

			// Restart multi-platform-controller pod to reload configMap again
			restartMultiPlatformControllerPod(f)

			dynamicInstanceTag = "ibmp-instance-" + util.GenerateRandomString(4)
			err = createConfigMapForIbmPDynamicInstance(f, dynamicInstanceTag)
			Expect(err).ShouldNot(HaveOccurred())

			err = createSecretsForIbmDynamicInstance(f)
			Expect(err).ShouldNot(HaveOccurred())

			component, applicationName, componentName = createApplicationAndComponent(f, testNamespace, "PPC64LE", "", "")
		})

		When("the Component with multi-platform-build is created", func() {
			It("a PipelineRun is triggered", func() {
				validatePipelineRunIsRunning(f, componentName, applicationName, testNamespace)
			})

			It("the build-container task from component pipelinerun is buildah-remote", func() {
				multiPlatformTaskName, multiPlatformSecretName = validateBuildContainerTaskIsBuildahRemote(f, componentName, applicationName, testNamespace)
			})

			It("The multi platform secret is populated", func() {
				instanceId = validateMultiPlatformSecretIsPopulated(f, testNamespace, multiPlatformTaskName, multiPlatformSecretName)
			})

			It("that PipelineRun completes successfully", func() {
				Expect(f.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "", f.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
			})

			It("check cleanup happened successfully", func() {
				Eventually(func() error {
					count, err := getIbmPDynamicInstanceCount(dynamicInstanceTag)
					if err != nil {
						return err
					}
					if count != 0 {
						return fmt.Errorf("instance is not cleaned up properly, running instances count: %d", count)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "timed out when verifying that the remote host was cleaned up correctly")
			})

		})
	})
})

// createApplicationAndComponent creates Applicaiton and Component CR from given Git URL and revision.
// createApplicationAndComponent returns a tuple with three values in order, the created Component CR, application name and component name.
func createApplicationAndComponent(f *framework.Framework, testNamespace, platform, gitUrl, gitRevision string) (*appservice.Component, string, string) {
    applicationName := fmt.Sprintf("multi-platform-suite-application-%s", util.GenerateRandomString(4))
	_, err := f.AsKubeAdmin.HasController.CreateApplication(applicationName, testNamespace)
	Expect(err).NotTo(HaveOccurred())

    componentName := fmt.Sprintf("multi-platform-suite-component-%s", util.GenerateRandomString(4))

    gitSourceUrl := gitUrl
    if gitSourceUrl == "" {
        gitSourceUrl = multiPlatformProjectGitUrl
    }

    gitSourceRevision := gitRevision
    if gitSourceRevision == "" {
        gitSourceRevision = multiPlatformProjectRevision
    }

	customBuildahRemotePipeline := os.Getenv(constants.CUSTOM_BUILDAH_REMOTE_PIPELINE_BUILD_BUNDLE_ENV + "_" + platform)
	Expect(customBuildahRemotePipeline).ShouldNot(BeEmpty())
	buildPipelineAnnotation := map[string]string{
		"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"buildah-remote-pipeline", "bundle": "%s"}`, customBuildahRemotePipeline),
	}

	// Create a component with Git Source URL being defined
	componentObj := appservice.ComponentSpec{
		ComponentName: componentName,
		Source: appservice.ComponentSource{
			ComponentSourceUnion: appservice.ComponentSourceUnion{
				GitSource: &appservice.GitSource{
					URL:      gitSourceUrl,
					Revision: gitSourceRevision,
					DockerfileURL: constants.DockerFilePath,
				},
			},
		},
	}
    component, err := f.AsKubeAdmin.HasController.CreateComponent(componentObj, testNamespace, "", "", applicationName, true, buildPipelineAnnotation)
	Expect(err).ShouldNot(HaveOccurred())
	return component, applicationName, componentName
}

func validateMultiPlatformSecretIsPopulated(f *framework.Framework, testNamespace, multiPlatformTaskName, multiPlatformSecretName string) (instanceId string) {
	Eventually(func() error {
		_, err := f.AsKubeAdmin.CommonController.GetSecret(testNamespace, multiPlatformSecretName)
		if err != nil {
			return err
		}
		return nil
	}, timeout, interval).Should(Succeed(), "timed out when verifying the secret is created")

	// Get the instance id from the task so that we can check during cleanup
	taskRun, err := f.AsKubeDeveloper.TektonController.GetTaskRun(multiPlatformTaskName, testNamespace)
	Expect(err).ShouldNot(HaveOccurred())
	instanceId = taskRun.Annotations["build.appstudio.redhat.com/cloud-instance-id"]
	GinkgoWriter.Printf("INSTANCE ID: %s\n", instanceId)
	Expect(instanceId).ShouldNot(BeEmpty())
	return
}

func validateBuildContainerTaskIsBuildahRemote(f *framework.Framework, componentName, applicationName, testNamespace string) (multiPlatformTaskName, multiPlatformSecretName string) {
	Eventually(func() error {
		pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
		Expect(err).ShouldNot(HaveOccurred())

		for _, chr := range pr.Status.ChildReferences {
			taskRun := &pipeline.TaskRun{}
			taskRunKey := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
			err := f.AsKubeAdmin.CommonController.KubeRest().Get(context.TODO(), taskRunKey, taskRun)
			Expect(err).ShouldNot(HaveOccurred())

			prTrStatus := &pipeline.PipelineRunTaskRunStatus{
				PipelineTaskName: chr.PipelineTaskName,
				Status:           &taskRun.Status,
			}

			if chr.PipelineTaskName == constants.BuildTaskRunName && prTrStatus.Status != nil && prTrStatus.Status.TaskSpec != nil && prTrStatus.Status.TaskSpec.Volumes != nil {
				multiPlatformTaskName = chr.Name
				for _, vol := range prTrStatus.Status.TaskSpec.Volumes {
					if vol.Secret != nil && strings.HasPrefix(vol.Secret.SecretName, "multi-platform-ssh-") {
						multiPlatformSecretName = vol.Secret.SecretName
						return nil
					}
				}
			}
		}
		return fmt.Errorf("couldn't find a matching step buildah-remote or ssh secret attached as a volume in the task %s in PipelineRun %s/%s", constants.BuildTaskRunName, testNamespace, pr.GetName())
	}, timeout, interval).Should(Succeed(), "timed out when verifying the buildah-remote image reference in pipelinerun")
	return
}

func validatePipelineRunIsRunning(f *framework.Framework, componentName, applicationName, testNamespace string) {
	Eventually(func() error {
		pr, err := f.AsKubeAdmin.HasController.GetComponentPipelineRun(componentName, applicationName, testNamespace, "")
		if err != nil {
			GinkgoWriter.Printf("PipelineRun has not been created yet for the component %s/%s", testNamespace, componentName)
			return err
		}
		if !pr.HasStarted() {
			return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
		}
		return nil
	}, timeout, constants.PipelineRunPollingInterval).Should(Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to start for the component %s/%s", testNamespace, componentName))
}

func restartMultiPlatformControllerPod(f *framework.Framework) {
	// Restart multi-platform-controller pod to reload configMap again
	podList, err := f.AsKubeAdmin.CommonController.ListAllPods(ControllerNamespace)
	Expect(err).ShouldNot(HaveOccurred())
	for i := range podList.Items {
		podName := podList.Items[i].Name
		if strings.HasPrefix(podName, ControllerNamespace) {
			err := f.AsKubeAdmin.CommonController.DeletePod(podName, ControllerNamespace)
			Expect(err).ShouldNot(HaveOccurred())
		}
	}
	time.Sleep(10 * time.Second)
	//check that multi-platform-controller pod is running
	Eventually(func() (bool, error) {
		podList, err := f.AsKubeAdmin.CommonController.ListAllPods(ControllerNamespace)
		if err != nil {
			return false, err
		}
		for i := range podList.Items {
			podName := podList.Items[i].Name
			if strings.HasPrefix(podName, ControllerNamespace) {
				pod, err := f.AsKubeAdmin.CommonController.GetPod(ControllerNamespace, podName)
				if err != nil {
					return false, err
				}
				if pod.Status.Phase == v1.PodRunning {
					return true, nil
				}
			}
		}
		return false, nil
	}, 1*time.Minute, 5*time.Second).Should(BeTrue(), "timed out while checking if the pod is running")
}

func pCloudId() string {
	return strings.Split(strings.Split(CRN, "/")[1], ":")[1]
}

func getIbmPDynamicInstanceCount(instanceTag string) (int, error) {
	apiKey := os.Getenv("MULTI_PLATFORM_IBM_API_KEY")
	serviceOptions := &core.ServiceOptions{
		URL: IbmPUrl,
		Authenticator: &core.IamAuthenticator{
			ApiKey: apiKey,
		},
	}
	baseService, err := core.NewBaseService(serviceOptions)
	if err != nil {
		return 0, err
	}

	builder := core.NewRequestBuilder(core.GET)
	builder = builder.WithContext(context.Background())
	builder.EnableGzipCompression = baseService.GetEnableGzipCompression()

	pathParamsMap := map[string]string{
		"cloud": pCloudId(),
	}
	_, err = builder.ResolveRequestURL(IbmPUrl, `/pcloud/v1/cloud-instances/{cloud}/pvm-instances`, pathParamsMap)
	if err != nil {
		return 0, err
	}
	builder.AddHeader("CRN", CRN)
	builder.AddHeader("Accept", "application/json")

	request, err := builder.Build()
	if err != nil {
		return 0, err
	}

	var rawResponse map[string]json.RawMessage
	_, err = baseService.Request(request, &rawResponse)
	if err != nil {
		return 0, err
	}
	instancesData := rawResponse["pvmInstances"]
	instances := []json.RawMessage{}
	err = json.Unmarshal(instancesData, &instances)
	if err != nil {
		return 0, err
	}
	count := 0
	type Instance struct {
		ServerName string
	}
	singleInstance := &Instance{}
	for i := range instances {
		err = json.Unmarshal(instances[i], singleInstance)
		if err != nil {
			return 0, err
		}
		if strings.HasPrefix(singleInstance.ServerName, instanceTag) {
			count++
		}
	}
	return count, nil
}

func terminateIbmPInstance(instanceId string) error {
	apiKey := os.Getenv("MULTI_PLATFORM_IBM_API_KEY")
	serviceOptions := &core.ServiceOptions{
		URL: IbmPUrl,
		Authenticator: &core.IamAuthenticator{
			ApiKey: apiKey,
		},
	}
	baseService, err := core.NewBaseService(serviceOptions)
	if err != nil {
		return err
	}

	builder := core.NewRequestBuilder(core.DELETE)
	builder = builder.WithContext(context.Background())
	builder.EnableGzipCompression = baseService.GetEnableGzipCompression()

	pathParamsMap := map[string]string{
		"cloud":           pCloudId(),
		"pvm_instance_id": instanceId,
	}
	_, err = builder.ResolveRequestURL(IbmPUrl, `/pcloud/v1/cloud-instances/{cloud}/pvm-instances/{pvm_instance_id}`, pathParamsMap)
	if err != nil {
		return err
	}
	builder.AddQuery("delete_data_volumes", "true")
	builder.AddHeader("CRN", CRN)
	builder.AddHeader("Accept", "application/json")

	request, err := builder.Build()
	if err != nil {
		return err
	}

	var rawResponse map[string]json.RawMessage
	_, err = baseService.Request(request, &rawResponse)
	if err != nil {
		if err.Error() == "pvm-instance not found" {
			return nil
		}
		return err
	}
	return nil
}
func getIbmZDynamicInstances(instanceTag string) ([]string, error) {
	apiKey := os.Getenv("MULTI_PLATFORM_IBM_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ibm api key is not set")
	}
	// Instantiate the service with an API key based IAM authenticator
	vpcService, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		URL: IbmZUrl,
		Authenticator: &core.IamAuthenticator{
			ApiKey: apiKey,
		},
	})
	if err != nil {
		return nil, err
	}
	// Lookup VPC
	vpcs, _, err := vpcService.ListVpcs(&vpcv1.ListVpcsOptions{})
	if err != nil {
		return nil, err
	}
	var vpc *vpcv1.VPC
	for i := range vpcs.Vpcs {
		//GinkgoWriter.Println("VPC: " + *vpcs.Vpcs[i].Name)
		if *vpcs.Vpcs[i].Name == IbmVpc {
			vpc = &vpcs.Vpcs[i]
			break
		}
	}
	if vpc == nil {
		return nil, fmt.Errorf("failed to find VPC %s", IbmVpc)
	}

	instances, _, err := vpcService.ListInstances(&vpcv1.ListInstancesOptions{ResourceGroupID: vpc.ResourceGroup.ID, VPCName: &IbmVpc})
	if err != nil {
		return nil, err
	}
	var instanceIds []string
	for _, instance := range instances.Instances {
		if strings.HasPrefix(*instance.Name, instanceTag) {
			instanceIds = append(instanceIds, *instance.ID)
		}
	}
	return instanceIds, nil
}

func terminateIbmZInstance(instanceId string) error {
	apiKey := os.Getenv("MULTI_PLATFORM_IBM_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ibm api key is not set correctly")
	}
	vpcService, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		URL: IbmZUrl,
		Authenticator: &core.IamAuthenticator{
			ApiKey: apiKey,
		},
	})
	if err != nil {
		return err
	}
	instance, _, err := vpcService.GetInstance(&vpcv1.GetInstanceOptions{ID: &instanceId})
	if err != nil {
		if err.Error() == "Instance not found" {
			return nil
		}
		GinkgoWriter.Printf("failed to delete system z instance, unable to get instance with error: %v\n", err)
		return err
	}
	_, err = vpcService.DeleteInstance(&vpcv1.DeleteInstanceOptions{ID: instance.ID})
	if err != nil {
		GinkgoWriter.Printf("failed to delete system z instance: %v\n", err)
		return err
	}
	return nil
}

func createConfigMapForIbmZDynamicInstance(f *framework.Framework, instanceTag string) error {
	hostConfig := &v1.ConfigMap{}
	hostConfig.Name = HostConfig
	hostConfig.Namespace = ControllerNamespace
	hostConfig.Labels = map[string]string{MultiPlatformConfigKey: "hosts"}

	hostConfig.Data = map[string]string{}
	hostConfig.Data["dynamic-platforms"] = "linux/s390x"
	hostConfig.Data["instance-tag"] = instanceTag
	hostConfig.Data["dynamic.linux-s390x.type"] = "ibmz"
	hostConfig.Data["dynamic.linux-s390x.ssh-secret"] = SshSecretName
	hostConfig.Data["dynamic.linux-s390x.secret"] = IbmSecretName
	hostConfig.Data["dynamic.linux-s390x.vpc"] = IbmVpc
	hostConfig.Data["dynamic.linux-s390x.key"] = IbmKey
	hostConfig.Data["dynamic.linux-s390x.subnet"] = "us-east-2-default-subnet"
	hostConfig.Data["dynamic.linux-s390x.image-id"] = "r014-17c957e0-01a1-4f7f-bc24-191f5f10eba8"
	hostConfig.Data["dynamic.linux-s390x.region"] = "us-east-2"
	hostConfig.Data["dynamic.linux-s390x.url"] = IbmZUrl
	hostConfig.Data["dynamic.linux-s390x.profile"] = "bz2-1x4"
	hostConfig.Data["dynamic.linux-s390x.max-instances"] = "1"

	_, err := f.AsKubeAdmin.CommonController.CreateConfigMap(hostConfig, ControllerNamespace)
	if err != nil {
		return fmt.Errorf("error while creating config map for dynamic instance: %v", err)
	}
	return nil
}

func createSecretsForIbmDynamicInstance(f *framework.Framework) error {
	ibmKey := v1.Secret{}
	ibmKey.Name = "ibmkey"
	ibmKey.Namespace = ControllerNamespace
	ibmKey.Labels = map[string]string{MultiPlatformSecretKey: "true"}
	ibmKey.StringData = map[string]string{
		"api-key": os.Getenv("MULTI_PLATFORM_IBM_API_KEY"),
	}
	_, err := f.AsKubeAdmin.CommonController.CreateSecret(ControllerNamespace, &ibmKey)
	if err != nil {
		return fmt.Errorf("error creating secret with api_key: %v", err)
	}

	sshKeys := v1.Secret{}
	sshKeys.Name = SshSecretName
	sshKeys.Namespace = ControllerNamespace
	sshKeys.Labels = map[string]string{MultiPlatformSecretKey: "true"}
	sshKeys.StringData = map[string]string{"id_rsa": os.Getenv("MULTI_PLATFORM_AWS_SSH_KEY")}
	_, err = f.AsKubeAdmin.CommonController.CreateSecret(ControllerNamespace, &sshKeys)
	if err != nil {
		return fmt.Errorf("error creating secret with ssh private key: %v", err)
	}
	return nil
}

func createConfigMapForIbmPDynamicInstance(f *framework.Framework, instanceTag string) error {
	hostConfig := &v1.ConfigMap{}
	hostConfig.Name = HostConfig
	hostConfig.Namespace = ControllerNamespace
	hostConfig.Labels = map[string]string{MultiPlatformConfigKey: "hosts"}

	hostConfig.Data = map[string]string{}
	hostConfig.Data["dynamic-platforms"] = "linux/ppc64le"
	hostConfig.Data["instance-tag"] = instanceTag
	hostConfig.Data["dynamic.linux-ppc64le.type"] = "ibmp"
	hostConfig.Data["dynamic.linux-ppc64le.ssh-secret"] = SshSecretName
	hostConfig.Data["dynamic.linux-ppc64le.secret"] = "ibmkey"
	hostConfig.Data["dynamic.linux-ppc64le.key"] = IbmKey
	hostConfig.Data["dynamic.linux-ppc64le.image"] = "sdouglas-rhel-test"
	hostConfig.Data["dynamic.linux-ppc64le.crn"] = CRN
	hostConfig.Data["dynamic.linux-ppc64le.url"] = IbmPUrl
	hostConfig.Data["dynamic.linux-ppc64le.network"] = "dff71085-73da-49f5-9bf2-5ea60c66c99b"
	hostConfig.Data["dynamic.linux-ppc64le.system"] = "e980"
	hostConfig.Data["dynamic.linux-ppc64le.cores"] = "0.25"
	hostConfig.Data["dynamic.linux-ppc64le.memory"] = "2"
	hostConfig.Data["dynamic.linux-ppc64le.max-instances"] = "2"

	_, err := f.AsKubeAdmin.CommonController.CreateConfigMap(hostConfig, ControllerNamespace)
	if err != nil {
		return fmt.Errorf("error while creating config map for dynamic instance: %v", err)
	}
	return nil
}

// Function to check if a file exists on the remote host
func dirExists(session *ssh.Session, dirPath string) bool {
	cmd := fmt.Sprintf("[ -d %s ] && echo 'exists'", dirPath)
	output, err := session.CombinedOutput(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running command: %s\n", err)
		return false
	}
	return string(output) == "exists\n"
}

// Get AWS instances that are running
// These are identified by tag
func getHostPoolAwsInstances() ([]string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(EnvCredentialsProvider{}),
		config.WithRegion(AwsRegion))
	if err != nil {
		return nil, err
	}

	// Create an EC2 client
	ec2Client := ec2.NewFromConfig(cfg)
	res, err := ec2Client.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{Filters: []ec2types.Filter{{Name: aws.String("tag:" + Ec2ArmTag), Values: []string{"true"}}}})
	if err != nil {
		return nil, err
	}
	ret := []string{}
	for _, res := range res.Reservations {
		for _, inst := range res.Instances {
			if inst.State.Name != ec2types.InstanceStateNameTerminated {
				ret = append(ret, *inst.PublicDnsName)
			}
		}
	}
	return ret, nil
}

func createConfigMapForHostPool(f *framework.Framework) error {
	armInstances, err := getHostPoolAwsInstances()
	if err != nil {
		return fmt.Errorf("error getting aws host pool instances: %v", err)
	}
	hostConfig := &v1.ConfigMap{}
	hostConfig.Name = HostConfig
	hostConfig.Namespace = ControllerNamespace
	hostConfig.Labels = map[string]string{MultiPlatformConfigKey: "hosts"}

	hostConfig.Data = map[string]string{}
	count := 0
	for _, instance := range armInstances {
		hostConfig.Data[fmt.Sprintf("host.aws-arm64-%d.address", count)] = instance
		hostConfig.Data[fmt.Sprintf("host.aws-arm64-%d.platform", count)] = AwsPlatform
		hostConfig.Data[fmt.Sprintf("host.aws-arm64-%d.user", count)] = Ec2User
		hostConfig.Data[fmt.Sprintf("host.aws-arm64-%d.secret", count)] = AwsSecretName
		hostConfig.Data[fmt.Sprintf("host.aws-arm64-%d.concurrency", count)] = "4"
		count++
	}

	_, err = f.AsKubeAdmin.CommonController.CreateConfigMap(hostConfig, ControllerNamespace)
	if err != nil {
		return fmt.Errorf("error creating host-pool config map: %v", err)
	}
	return nil
}

func createConfigMapForDynamicInstance(f *framework.Framework, instanceTag string) error {
	hostConfig := &v1.ConfigMap{}
	hostConfig.Name = HostConfig
	hostConfig.Namespace = ControllerNamespace
	hostConfig.Labels = map[string]string{MultiPlatformConfigKey: "hosts"}

	hostConfig.Data = map[string]string{}
	hostConfig.Data["dynamic-platforms"] = AwsPlatform
	hostConfig.Data["instance-tag"] = instanceTag
	hostConfig.Data["dynamic.linux-arm64.type"] = "aws"
	hostConfig.Data["dynamic.linux-arm64.region"] = AwsRegion
	hostConfig.Data["dynamic.linux-arm64.ami"] = "ami-09d5d0912f52f9514"
	hostConfig.Data["dynamic.linux-arm64.instance-type"] = "t4g.micro"
	hostConfig.Data["dynamic.linux-arm64.key-name"] = "multi-platform-e2e"
	hostConfig.Data["dynamic.linux-arm64.aws-secret"] = AwsSecretName
	hostConfig.Data["dynamic.linux-arm64.ssh-secret"] = SshSecretName
	hostConfig.Data["dynamic.linux-arm64.security-group"] = "launch-wizard-7"
	hostConfig.Data["dynamic.linux-arm64.max-instances"] = DynamicMaxInstances

	_, err := f.AsKubeAdmin.CommonController.CreateConfigMap(hostConfig, ControllerNamespace)
	if err != nil {
		return fmt.Errorf("error while creating config map for dynamic instance: %v", err)
	}
	return nil
}

func createSecretForHostPool(f *framework.Framework) error {
	keys := v1.Secret{}
	keys.Name = AwsSecretName
	keys.Namespace = ControllerNamespace
	keys.Labels = map[string]string{MultiPlatformSecretKey: "true"}
	keys.StringData = map[string]string{"id_rsa": os.Getenv("MULTI_PLATFORM_AWS_SSH_KEY")}
	_, err := f.AsKubeAdmin.CommonController.CreateSecret(ControllerNamespace, &keys)
	if err != nil {
		return fmt.Errorf("error while creating host-pool secret: %v", err)
	}
	return nil
}

func createSecretsForDynamicInstance(f *framework.Framework) error {
	awsKeys := v1.Secret{}
	awsKeys.Name = AwsSecretName
	awsKeys.Namespace = ControllerNamespace
	awsKeys.Labels = map[string]string{MultiPlatformSecretKey: "true"}
	awsKeys.StringData = map[string]string{
		"access-key-id":     os.Getenv("MULTI_PLATFORM_AWS_ACCESS_KEY"),
		"secret-access-key": os.Getenv("MULTI_PLATFORM_AWS_SECRET_ACCESS_KEY"),
	}
	_, err := f.AsKubeAdmin.CommonController.CreateSecret(ControllerNamespace, &awsKeys)
	if err != nil {
		return fmt.Errorf("error creating secret with access_key and secret_key: %v", err)
	}

	sshKeys := v1.Secret{}
	sshKeys.Name = SshSecretName
	sshKeys.Namespace = ControllerNamespace
	sshKeys.Labels = map[string]string{MultiPlatformSecretKey: "true"}
	sshKeys.StringData = map[string]string{"id_rsa": os.Getenv("MULTI_PLATFORM_AWS_SSH_KEY")}
	_, err = f.AsKubeAdmin.CommonController.CreateSecret(ControllerNamespace, &sshKeys)
	if err != nil {
		return fmt.Errorf("error creating secret with ssh private key: %v", err)
	}
	return nil
}

func terminateAwsInstance(instanceId string) error {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(EnvCredentialsProvider{}),
		config.WithRegion(AwsRegion))
	if err != nil {
		return err
	}
	// Create an EC2 client
	ec2Client := ec2.NewFromConfig(cfg)
	//Terminate Instance
	_, err = ec2Client.TerminateInstances(context.TODO(), &ec2.TerminateInstancesInput{InstanceIds: []string{string(instanceId)}})
	return err
}

func getDynamicAwsInstance(tagName string) ([]string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(EnvCredentialsProvider{}),
		config.WithRegion(AwsRegion))
	if err != nil {
		return nil, err
	}

	// Create an EC2 client
	ec2Client := ec2.NewFromConfig(cfg)
	res, err := ec2Client.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{Filters: []ec2types.Filter{{Name: aws.String("tag:" + "multi-platform-instance"), Values: []string{tagName}}}})
	if err != nil {
		return nil, err
	}
	ret := []string{}
	for _, res := range res.Reservations {
		for _, inst := range res.Instances {
			if inst.State.Name != ec2types.InstanceStateNameTerminated {
				ret = append(ret, *inst.PublicDnsName)
			}
		}
	}
	return ret, nil
}

type EnvCredentialsProvider struct {
}

func (r EnvCredentialsProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: os.Getenv("MULTI_PLATFORM_AWS_ACCESS_KEY"), SecretAccessKey: os.Getenv("MULTI_PLATFORM_AWS_SECRET_ACCESS_KEY")}, nil
}
