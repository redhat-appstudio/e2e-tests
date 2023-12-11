package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uitable/util/strutil"
	metricsConstants "github.com/redhat-appstudio-qe/perf-monitoring/api/pkg/constants"
	"github.com/redhat-appstudio-qe/perf-monitoring/api/pkg/metrics"
	appstudioApi "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	loadtestUtils "github.com/redhat-appstudio/e2e-tests/pkg/utils/loadtests"
	integrationv1beta1 "github.com/redhat-appstudio/integration-service/api/v1beta1"
	"github.com/spf13/cobra"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
)

var (
	componentRepoUrl              string = "https://github.com/devfile-samples/devfile-sample-code-with-quarkus"
	usernamePrefix                string = "testuser"
	numberOfUsers                 int
	testScenarioGitURL            string = "https://github.com/redhat-appstudio/integration-examples.git"
	testScenarioRevision          string = "main"
	testScenarioPathInRepo        string = "pipelines/integration_resolver_pipeline_pass.yaml"
	waitPipelines                 bool
	waitDeployments               bool
	waitIntegrationTestsPipelines bool
	verbose                       bool
	logConsole                    bool
	failFast                      bool
	disableMetrics                bool
	threadCount                   int
	randomString                  bool
	pipelineSkipInitialChecks     bool
	stage                         bool
	outputDir                     string = "."
	enableProgressBars            bool
	pushGatewayURI                string = ""
	jobName                       string = ""
)

var (
	UserCreationTimeMaxPerThread         []time.Duration
	ApplicationCreationTimeMaxPerThread  []time.Duration
	ItsCreationTimeMaxPerThread          []time.Duration
	CDQCreationTimeMaxPerThread          []time.Duration
	ComponentCreationTimeMaxPerThread    []time.Duration
	PipelineRunSucceededTimeMaxPerThread []time.Duration

	DeploymentSucceededTimeMaxPerThread                  []time.Duration
	IntegrationTestsPipelineRunSucceededTimeMaxPerThread []time.Duration

	UserCreationTimeSumPerThread         []time.Duration
	ApplicationCreationTimeSumPerThread  []time.Duration
	ItsCreationTimeSumPerThread          []time.Duration
	CDQCreationTimeSumPerThread          []time.Duration
	ComponentCreationTimeSumPerThread    []time.Duration
	PipelineRunSucceededTimeSumPerThread []time.Duration
	PipelineRunFailedTimeSumPerThread    []time.Duration
	PipelineRunWaitTimeForPVCSumPerThread[]time.Duration

	DeploymentSucceededTimeSumPerThread                  []time.Duration
	DeploymentFailedTimeSumPerThread                     []time.Duration
	IntegrationTestsPipelineRunSucceededTimeSumPerThread []time.Duration
	IntegrationTestsPipelineRunFailedTimeSumPerThread    []time.Duration

	SuccessfulUserCreationsPerThread                []int64
	SuccessfulApplicationCreationsPerThread         []int64
	SuccessfulItsCreationsPerThread                 []int64
	SuccessfulCDQCreationsPerThread                 []int64
	SuccessfulComponentCreationsPerThread           []int64
	SuccessfulPipelineRunsPerThread                 []int64
	SuccessfulDeploymentsPerThread                  []int64
	SuccessfulIntegrationTestsPipelineRunsPerThread []int64
	SuccessfulPVCCreationsPerThread                 []int64

	FailedUserCreationsPerThread                []int64
	FailedApplicationCreationsPerThread         []int64
	FailedItsCreationsPerThread                 []int64
	FailedCDQCreationsPerThread                 []int64
	FailedComponentCreationsPerThread           []int64
	FailedPipelineRunsPerThread                 []int64
	FailedDeploymentsPerThread                  []int64
	FailedIntegrationTestsPipelineRunsPerThread []int64

	frameworkMap                      *sync.Map
	userComponentMap                  *sync.Map
	userTestScenarioMap               *sync.Map
	userComponentPipelineRunMap       *sync.Map
	errorCountMap                     map[int]ErrorCount
	errorMutex                        = &sync.Mutex{}
	usersBarMutex                     = &sync.Mutex{}
	applicationsBarMutex              = &sync.Mutex{}
	itsBarMutex                       = &sync.Mutex{}
	cdqsBarMutex                      = &sync.Mutex{}
	componentsBarMutex                = &sync.Mutex{}
	pipelinesBarMutex                 = &sync.Mutex{}
	deploymentsBarMutex               = &sync.Mutex{}
	integrationTestsPipelinesBarMutex = &sync.Mutex{}
	threadsWG                         *sync.WaitGroup
	logData                           LogData
	stageUsers                        []loadtestUtils.User
	selectedUsers                     []loadtestUtils.User
	CI                                bool
	JobName                           string
	MetricsController                 *metrics.MetricsPush
)

type ErrorOccurrence struct {
	ErrorCode int    `json:"errorCode"`
	Message   string `json:"message"`
}

type ErrorCount struct {
	ErrorCode int `json:"errorCode"`
	Count     int `json:"count"`
}

type LogData struct {
	Timestamp                         string  `json:"timestamp"`
	EndTimestamp                      string  `json:"endTimestamp"`
	MachineName                       string  `json:"machineName"`
	BinaryDetails                     string  `json:"binaryDetails"`
	ComponentRepoUrl                  string  `json:"componentRepoUrl"`
	NumberOfThreads                   int     `json:"threads"`
	NumberOfUsersPerThread            int     `json:"usersPerThread"`
	NumberOfUsers                     int     `json:"totalUsers"`
	PipelineSkipInitialChecks         bool    `json:"pipelineSkipInitialChecks"`
	LoadTestCompletionStatus          string  `json:"status"`
	AverageTimeToSpinUpUsers          float64 `json:"createUserTimeAvg"`
	MaxTimeToSpinUpUsers              float64 `json:"createUserTimeMax"`
	AverageTimeToCreateApplications   float64 `json:"createApplicationsTimeAvg"`
	MaxTimeToCreateApplications       float64 `json:"createApplicationsTimeMax"`
	AverageTimeToCreateIts            float64 `json:"createItsTimeAvg"`
	MaxTimeToCreateIts                float64 `json:"createItsTimeMax"`
	AverageTimeToCreateCDQs           float64 `json:"createCDQsTimeAvg"`
	MaxTimeToCreateCDQs               float64 `json:"createCDQsTimeMax"`
	AverageTimeToCreateComponents     float64 `json:"createComponentsTimeAvg"`
	MaxTimeToCreateComponents         float64 `json:"createComponentsTimeMax"`
	AverageTimeToRunPipelineSucceeded float64 `json:"runPipelineSucceededTimeAvg"`
	MaxTimeToRunPipelineSucceeded     float64 `json:"runPipelineSucceededTimeMax"`
	AverageTimeToRunPipelineFailed    float64 `json:"runPipelineFailedTimeAvg"`
	AverageWaitTimeForPVCProvisioning float64 `json:"WaitTimeForPVCProvisioningAvg"`

	AverageTimeToDeploymentSucceeded float64 `json:"deploymentSucceededTimeAvg"`
	MaxTimeToDeploymentSucceeded     float64 `json:"deploymentSucceededTimeMax"`
	AverageTimeToDeploymentFailed    float64 `json:"deploymentFailedTimeAvg"`

	IntegrationTestsAverageTimeToRunPipelineSucceeded float64 `json:"integrationTestsRunPipelineSucceededTimeAvg"`
	IntegrationTestsMaxTimeToRunPipelineSucceeded     float64 `json:"integrationTestsRunPipelineSucceededTimeMax"`
	IntegrationTestsAverageTimeToRunPipelineFailed    float64 `json:"integrationTestsRunPipelineFailedTimeAvg"`

	UserCreationSuccessCount        int64   `json:"createUserSuccesses"`
	UserCreationFailureCount        int64   `json:"createUserFailures"`
	UserCreationFailureRate         float64 `json:"createUserFailureRate"`
	ApplicationCreationSuccessCount int64   `json:"createApplicationsSuccesses"`
	ApplicationCreationFailureCount int64   `json:"createApplicationsFailures"`
	ApplicationCreationFailureRate  float64 `json:"createApplicationsFailureRate"`
	ItsCreationSuccessCount         int64   `json:"createItsSuccesses"`
	ItsCreationFailureCount         int64   `json:"createItsFailures"`
	ItsCreationFailureRate          float64 `json:"createItsFailureRate"`
	CDQCreationSuccessCount         int64   `json:"createCDQsSuccesses"`
	CDQCreationFailureCount         int64   `json:"createCDQsFailures"`
	CDQCreationFailureRate          float64 `json:"createCDQsFailureRate"`
	ComponentCreationSuccessCount   int64   `json:"createComponentsSuccesses"`
	ComponentCreationFailureCount   int64   `json:"createComponentsFailures"`
	ComponentCreationFailureRate    float64 `json:"createComponentsFailureRate"`
	PipelineRunSuccessCount         int64   `json:"runPipelineSuccesses"`
	PipelineRunFailureCount         int64   `json:"runPipelineFailures"`
	PipelineRunFailureRate          float64 `json:"runPipelineFailureRate"`
	PVCCreationSuccessCount         int64   `json:"createPVCSuccesses"`

	DeploymentSuccessCount int64   `json:"deploymentSuccesses"`
	DeploymentFailureCount int64   `json:"deploymentFailures"`
	DeploymentFailureRate  float64 `json:"deploymentFailureRate"`

	IntegrationTestsPipelineRunSuccessCount int64   `json:"integrationTestsRunPipelineSuccesses"`
	IntegrationTestsPipelineRunFailureCount int64   `json:"integrationTestsRunPipelineFailures"`
	IntegrationTestsPipelineRunFailureRate  float64 `json:"integrationTestsRunPipelineFailureRate"`

	WorkloadKPI float64 `json:"workloadKPI"`

	ErrorCounts []ErrorCount      `json:"errorCounts"`
	Errors      []ErrorOccurrence `json:"errors"`
	ErrorsTotal int               `json:"errorsTotal"`
}

func createLogDataJSON(outputFile string, logDataInput LogData) error {
	jsonData, err := json.MarshalIndent(logDataInput, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %v", err)
	}

	err = os.WriteFile(outputFile, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("error writing JSON file: %v", err)
	}

	return nil
}

var rootCmd = &cobra.Command{
	Use:           "load-test",
	Short:         "Used to Generate Users and Run Load Tests on AppStudio.",
	Long:          `Used to Generate Users and Run Load Tests on AppStudio.`,
	SilenceErrors: true,
	SilenceUsage:  false,
	Args:          cobra.NoArgs,
	Run:           setup,
}

var AppStudioUsersBar *uiprogress.Bar
var ApplicationsBar *uiprogress.Bar
var itsBar *uiprogress.Bar
var CDQsBar *uiprogress.Bar
var ComponentsBar *uiprogress.Bar
var PipelinesBar *uiprogress.Bar
var IntegrationTestsPipelinesBar *uiprogress.Bar
var DeploymentsBar *uiprogress.Bar

func ExecuteLoadTest() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// generate random string from charset = "abcdefghijklmnopqrstuvwxyz0123456789"
// We shall use length = 5 characters length random string with 60,466,176 random combinations
const customCharset = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomStringFromCharset(length int) string {
	var result []byte
	for i := 0; i < length; i++ {
		index := rand.Intn(len(customCharset))
		result = append(result, customCharset[index])
	}
	return string(result)
}

func init() {
	rootCmd.Flags().StringVar(&componentRepoUrl, "component-repo", componentRepoUrl, "the component repo URL to be used")
	rootCmd.Flags().StringVar(&usernamePrefix, "username", usernamePrefix, "the prefix used for usersignup names")
	// TODO use a custom kubeconfig and introduce debug logging and trace
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "if 'debug' traces should be displayed in the console")
	rootCmd.Flags().BoolVarP(&stage, "stage", "s", false, "is you want to run the test on stage")
	rootCmd.Flags().IntVarP(&numberOfUsers, "users", "u", 5, "the number of user accounts to provision per thread")
	rootCmd.Flags().StringVar(&testScenarioGitURL, "test-scenario-git-url", testScenarioGitURL, "test scenario GIT URL")
	rootCmd.Flags().StringVar(&testScenarioRevision, "test-scenario-revision", testScenarioRevision, "test scenario GIT URL repo revision to use")
	rootCmd.Flags().StringVar(&testScenarioPathInRepo, "test-scenario-path-in-repo", testScenarioPathInRepo, "test scenario path in GIT repo")
	rootCmd.Flags().BoolVarP(&waitPipelines, "waitpipelines", "w", false, "if you want to wait for pipelines to finish")
	rootCmd.Flags().BoolVarP(&waitIntegrationTestsPipelines, "waitintegrationtestspipelines", "i", false, "if you want to wait for IntegrationTests (Integration Test Scenario) pipelines to finish")
	rootCmd.Flags().BoolVarP(&waitDeployments, "waitdeployments", "d", false, "if you want to wait for deployments to finish")
	rootCmd.Flags().BoolVarP(&logConsole, "log-to-console", "l", false, "if you want to log to console in addition to the log file")
	rootCmd.Flags().BoolVar(&failFast, "fail-fast", false, "if you want the test to fail fast at first failure")
	rootCmd.Flags().BoolVar(&disableMetrics, "disable-metrics", false, "if you want to disable metrics gathering")
	rootCmd.Flags().IntVarP(&threadCount, "threads", "t", 1, "number of concurrent threads to execute")
	rootCmd.Flags().BoolVarP(&randomString, "randomstring", "r", false, "if you want to add random string to the user prefix")
	rootCmd.Flags().BoolVar(&pipelineSkipInitialChecks, "pipeline-skip-initial-checks", true, "if pipeline runs' initial checks are to be skipped")
	rootCmd.Flags().StringVarP(&outputDir, "output-dir", "o", ".", "directory where output files such as load-tests.log or load-tests.json are stored")
	rootCmd.Flags().BoolVar(&enableProgressBars, "enable-progress-bars", false, "if you want to enable progress bars")
	rootCmd.Flags().StringVar(&pushGatewayURI, "pushgateway-url", pushGatewayURI, "PushGateway url (needs to be set if metrics are enabled)")
	rootCmd.Flags().StringVar(&jobName, "job-name", jobName, "Job Name to track Metrics (needs to be set if metrics are enabled)")
}

func logError(errCode int, message string) {
	msg := fmt.Sprintf("Error #%d: %s", errCode, message)
	if failFast {
		klog.Fatalln(msg)
	} else {
		klog.Errorln(msg)
	}
	errorMutex.Lock()
	defer errorMutex.Unlock()

	errorCount, ok := errorCountMap[errCode]
	if ok {
		errorCount.Count = errorCount.Count + 1
		errorCountMap[errCode] = errorCount
	} else {
		errorCountMap[errCode] = ErrorCount{
			ErrorCode: errCode,
			Count:     1,
		}
	}

	errorOccurrence := ErrorOccurrence{
		ErrorCode: errCode,
		Message:   message,
	}
	logData.Errors = append(logData.Errors, errorOccurrence)
}

func setKlogFlag(fs flag.FlagSet, name string, value string) {
	err := fs.Set(name, value)
	if err != nil {
		klog.Fatalf("Unable to set klog flag %s: %v", name, err)
	}
}

func setup(cmd *cobra.Command, args []string) {
	cmd.SilenceUsage = true

	JobName = loadtestUtils.GetJobName(jobName)

	// waitDeployments sets waitIntegrationTestsPipelines=true implicitly
	waitIntegrationTestsPipelines = waitIntegrationTestsPipelines || waitDeployments

	// waitIntegrationTestsPipelines sets waitPipelines=true implicitly
	waitPipelines = waitPipelines || waitIntegrationTestsPipelines

	logFile, err := os.Create(fmt.Sprintf("%s/load-tests.log", outputDir))
	if err != nil {
		klog.Fatalf("Error creating log file: %v", err)
	}
	var fs flag.FlagSet
	klog.InitFlags(&fs)
	setKlogFlag(fs, "log_file", logFile.Name())
	setKlogFlag(fs, "logtostderr", "false")
	setKlogFlag(fs, "alsologtostderr", strconv.FormatBool(logConsole))

	overallCount := numberOfUsers * threadCount

	klog.Infof("Number of threads: %d", threadCount)
	klog.Infof("Number of users per thread: %d", numberOfUsers)
	klog.Infof("Number of users overall: %d", overallCount)
	klog.Infof("Pipeline run initial checks skipped: %t", pipelineSkipInitialChecks)

	klog.Infof("🕖 initializing...\n")

	if !disableMetrics {
		// add metrics releated code here
		if loadtestUtils.UrlCheck(pushGatewayURI) {
			klog.Infof("Init Metrics")
			MetricsController = metrics.NewMetricController(pushGatewayURI, JobName)
			MetricsController.InitPusher()
		} else {
			klog.Fatalf("The Right PushGateway URL is required if metrics are enabled!")
		}
	}

	if stage {
		klog.Infof("Loading Stage Users...\n")
		// Debug
		klog.Infof("** Stage Users - %s ...\n", constants.JsonStageUsersPath)
		stageUsers, err = loadtestUtils.LoadStageUsers(constants.JsonStageUsersPath)
		// Debug

		if err != nil {
			klog.Fatalf("Error Loading Stage Users from the given Path Please check file/contents exists: %v", err)
		}

		// Debug
		klog.Infof("** Stage Users - %s ...\n", constants.JsonStageUsersPath)

		selectedUsers, err = loadtestUtils.SelectUsers(stageUsers, numberOfUsers, threadCount, len(stageUsers))
		// Debug
		klog.Infof("** numberOfUsers - %d; len(stageUsers) - %d ...\n", numberOfUsers, len(stageUsers))
		klog.Infof("** selectedUsers - %v ...\n", selectedUsers)

		if err != nil {
			klog.Fatalf("Error Selecting the Users Based on thread count: %v", err)
		}
	}

	machineName, err := os.Hostname()
	if err != nil {
		klog.Errorf("error getting hostname: %v\n", err)
		return
	}

	goVersion := runtime.Version()
	goOS := runtime.GOOS
	goArch := runtime.GOARCH
	binaryDetails := fmt.Sprintf("Built with %s for %s/%s", goVersion, goOS, goArch)

	logData = LogData{
		Timestamp:                 time.Now().Format("2006-01-02T15:04:05Z07:00"),
		MachineName:               machineName,
		BinaryDetails:             binaryDetails,
		ComponentRepoUrl:          componentRepoUrl,
		NumberOfThreads:           threadCount,
		NumberOfUsersPerThread:    numberOfUsers,
		NumberOfUsers:             overallCount,
		PipelineSkipInitialChecks: pipelineSkipInitialChecks,
		Errors:                    []ErrorOccurrence{},
		ErrorCounts:               []ErrorCount{},
	}

	klog.Infof("🍿 provisioning users...\n")

	uip := uiprogress.New()
	uip.Start()

	barLength := 60

	if enableProgressBars {
		userProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
			return strutil.PadLeft(fmt.Sprintf("Creating AppStudio Users (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedUserCreationsPerThread)), barLength, ' ')
		})
		AppStudioUsersBar = userProgress

		applicationProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
			return strutil.PadLeft(fmt.Sprintf("Creating AppStudio Applications (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedApplicationCreationsPerThread)), barLength, ' ')
		})
		ApplicationsBar = applicationProgress

		itsProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
			return strutil.PadLeft(fmt.Sprintf("Creating AppStudio Integration Test Scenarios (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedItsCreationsPerThread)), barLength, ' ')
		})
		itsBar = itsProgress

		cdqProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
			return strutil.PadLeft(fmt.Sprintf("Creating AppStudio CDQs (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedCDQCreationsPerThread)), barLength, ' ')
		})
		CDQsBar = cdqProgress

		componentProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
			return strutil.PadLeft(fmt.Sprintf("Creating AppStudio Components (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedComponentCreationsPerThread)), barLength, ' ')
		})
		ComponentsBar = componentProgress

		if waitPipelines {
			pipelineProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
				return strutil.PadLeft(fmt.Sprintf("Waiting for pipelines to finish (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedPipelineRunsPerThread)), barLength, ' ')
			})
			PipelinesBar = pipelineProgress
		}

		if waitIntegrationTestsPipelines {
			integrationTestProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
				return strutil.PadLeft(fmt.Sprintf("Waiting for integration tests to finish (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedIntegrationTestsPipelineRunsPerThread)), barLength, ' ')
			})
			IntegrationTestsPipelinesBar = integrationTestProgress
		}

		if waitDeployments {
			deploymentProgress := uip.AddBar(overallCount).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
				return strutil.PadLeft(fmt.Sprintf("Waiting for deployments to finish (%d/%d) [%d failed]", b.Current(), overallCount, sumFromArray(FailedDeploymentsPerThread)), barLength, ' ')
			})
			DeploymentsBar = deploymentProgress
		}
	} else {
		klog.Infoln("Progress bars are disabled by default. Please hold off until all iterations has completed. To enable the progress bars run with the --enable-progress-bars in [OPTIONS]")
	}

	UserCreationTimeMaxPerThread = make([]time.Duration, threadCount)
	ApplicationCreationTimeMaxPerThread = make([]time.Duration, threadCount)
	ItsCreationTimeMaxPerThread = make([]time.Duration, threadCount)
	CDQCreationTimeMaxPerThread = make([]time.Duration, threadCount)
	ComponentCreationTimeMaxPerThread = make([]time.Duration, threadCount)
	PipelineRunSucceededTimeMaxPerThread = make([]time.Duration, threadCount)

	DeploymentSucceededTimeMaxPerThread = make([]time.Duration, threadCount)
	IntegrationTestsPipelineRunSucceededTimeMaxPerThread = make([]time.Duration, threadCount)

	UserCreationTimeSumPerThread = make([]time.Duration, threadCount)
	ApplicationCreationTimeSumPerThread = make([]time.Duration, threadCount)
	ItsCreationTimeSumPerThread = make([]time.Duration, threadCount)
	CDQCreationTimeSumPerThread = make([]time.Duration, threadCount)
	ComponentCreationTimeSumPerThread = make([]time.Duration, threadCount)
	PipelineRunSucceededTimeSumPerThread = make([]time.Duration, threadCount)
	PipelineRunFailedTimeSumPerThread = make([]time.Duration, threadCount)
	PipelineRunWaitTimeForPVCSumPerThread = make([]time.Duration, threadCount)

	DeploymentSucceededTimeSumPerThread = make([]time.Duration, threadCount)
	DeploymentFailedTimeSumPerThread = make([]time.Duration, threadCount)

	IntegrationTestsPipelineRunSucceededTimeSumPerThread = make([]time.Duration, threadCount)
	IntegrationTestsPipelineRunFailedTimeSumPerThread = make([]time.Duration, threadCount)

	SuccessfulUserCreationsPerThread = make([]int64, threadCount)
	SuccessfulApplicationCreationsPerThread = make([]int64, threadCount)
	SuccessfulItsCreationsPerThread = make([]int64, threadCount)
	SuccessfulCDQCreationsPerThread = make([]int64, threadCount)
	SuccessfulComponentCreationsPerThread = make([]int64, threadCount)
	SuccessfulPipelineRunsPerThread = make([]int64, threadCount)
	SuccessfulPVCCreationsPerThread = make([]int64, threadCount)

	SuccessfulDeploymentsPerThread = make([]int64, threadCount)
	SuccessfulIntegrationTestsPipelineRunsPerThread = make([]int64, threadCount)

	FailedUserCreationsPerThread = make([]int64, threadCount)
	FailedApplicationCreationsPerThread = make([]int64, threadCount)
	FailedItsCreationsPerThread = make([]int64, threadCount)
	FailedCDQCreationsPerThread = make([]int64, threadCount)
	FailedComponentCreationsPerThread = make([]int64, threadCount)
	FailedPipelineRunsPerThread = make([]int64, threadCount)

	FailedDeploymentsPerThread = make([]int64, threadCount)
	FailedIntegrationTestsPipelineRunsPerThread = make([]int64, threadCount)

	frameworkMap = &sync.Map{}
	userComponentMap = &sync.Map{}
	userTestScenarioMap = &sync.Map{}
	userComponentPipelineRunMap = &sync.Map{}
	errorCountMap = make(map[int]ErrorCount)

	rand.Seed(time.Now().UnixNano())
	threadsWG = &sync.WaitGroup{}
	threadsWG.Add(threadCount)

	for thread := 0; thread < threadCount; thread++ {
		go userJourneyThread(frameworkMap, threadsWG, thread, AppStudioUsersBar, ApplicationsBar, itsBar, CDQsBar, ComponentsBar, PipelinesBar, IntegrationTestsPipelinesBar, DeploymentsBar)
	}

	threadsWG.Wait()
	uip.Stop()

	logData.EndTimestamp = time.Now().Format("2006-01-02T15:04:05Z07:00")

	logData.LoadTestCompletionStatus = "Completed"

	// Compiling data about UserSignups
	userCreationSuccessCount := sumFromArray(SuccessfulUserCreationsPerThread)
	logData.UserCreationSuccessCount = userCreationSuccessCount

	userCreationFailureCount := sumFromArray(FailedUserCreationsPerThread)
	logData.UserCreationFailureCount = userCreationFailureCount

	averageTimeToSpinUpUsers := float64(0)
	if userCreationSuccessCount > 0 {
		averageTimeToSpinUpUsers = sumDurationFromArray(UserCreationTimeSumPerThread).Seconds() / float64(userCreationSuccessCount)
	}
	logData.AverageTimeToSpinUpUsers = averageTimeToSpinUpUsers

	logData.MaxTimeToSpinUpUsers = maxDurationFromArray(UserCreationTimeMaxPerThread).Seconds()

	PVCCreationSuccessCount := sumFromArray(SuccessfulPVCCreationsPerThread)
	logData.PVCCreationSuccessCount = PVCCreationSuccessCount

	averageWaitTimeForPVCProvisioning := float64(0)
	averageWaitTimeForPVCProvisioning = sumDurationFromArray(PipelineRunWaitTimeForPVCSumPerThread).Seconds() / float64(PVCCreationSuccessCount)
	logData.AverageWaitTimeForPVCProvisioning = averageWaitTimeForPVCProvisioning

	userCreationFailureRate := float64(userCreationFailureCount) / float64(overallCount)
	logData.UserCreationFailureRate = userCreationFailureRate

	// Compiling data about Applications
	applicationCreationSuccessCount := sumFromArray(SuccessfulApplicationCreationsPerThread)
	logData.ApplicationCreationSuccessCount = applicationCreationSuccessCount

	applicationCreationFailureCount := sumFromArray(FailedApplicationCreationsPerThread)
	logData.ApplicationCreationFailureCount = applicationCreationFailureCount

	averageTimeToCreateApplications := float64(0)
	if applicationCreationSuccessCount > 0 {
		averageTimeToCreateApplications = sumDurationFromArray(ApplicationCreationTimeSumPerThread).Seconds() / float64(applicationCreationSuccessCount)
	}
	logData.AverageTimeToCreateApplications = averageTimeToCreateApplications

	logData.MaxTimeToCreateApplications = maxDurationFromArray(ApplicationCreationTimeMaxPerThread).Seconds()

	applicationCreationFailureRate := float64(applicationCreationFailureCount) / float64(overallCount)
	logData.ApplicationCreationFailureRate = applicationCreationFailureRate

	// Compiling data about Its
	itsCreationSuccessCount := sumFromArray(SuccessfulItsCreationsPerThread)
	logData.ItsCreationSuccessCount = itsCreationSuccessCount

	itsCreationFailureCount := sumFromArray(FailedItsCreationsPerThread)
	logData.ItsCreationFailureCount = itsCreationFailureCount

	averageTimeToCreateIts := float64(0)
	if itsCreationSuccessCount > 0 {
		averageTimeToCreateIts = sumDurationFromArray(ItsCreationTimeSumPerThread).Seconds() / float64(itsCreationSuccessCount)
	}
	logData.AverageTimeToCreateIts = averageTimeToCreateIts

	logData.MaxTimeToCreateIts = maxDurationFromArray(ItsCreationTimeMaxPerThread).Seconds()

	itsCreationFailureRate := float64(itsCreationFailureCount) / float64(overallCount)
	logData.ItsCreationFailureRate = itsCreationFailureRate

	// Compiling data about CDQs
	cdqCreationSuccessCount := sumFromArray(SuccessfulCDQCreationsPerThread)
	logData.CDQCreationSuccessCount = cdqCreationSuccessCount

	cdqCreationFailureCount := sumFromArray(FailedCDQCreationsPerThread)
	logData.CDQCreationFailureCount = cdqCreationFailureCount

	averageTimeToCreateCDQs := float64(0)
	if cdqCreationSuccessCount > 0 {
		averageTimeToCreateCDQs = sumDurationFromArray(CDQCreationTimeSumPerThread).Seconds() / float64(cdqCreationSuccessCount)
	}
	logData.AverageTimeToCreateCDQs = averageTimeToCreateCDQs

	logData.MaxTimeToCreateCDQs = maxDurationFromArray(CDQCreationTimeMaxPerThread).Seconds()

	cdqCreationFailureRate := float64(cdqCreationFailureCount) / float64(overallCount)
	logData.CDQCreationFailureRate = cdqCreationFailureRate

	// Compiling data about Components
	componentCreationSuccessCount := sumFromArray(SuccessfulComponentCreationsPerThread)
	logData.ComponentCreationSuccessCount = componentCreationSuccessCount

	componentCreationFailureCount := sumFromArray(FailedComponentCreationsPerThread)
	logData.ComponentCreationFailureCount = componentCreationFailureCount

	averageTimeToCreateComponents := float64(0)
	if componentCreationSuccessCount > 0 {
		averageTimeToCreateComponents = sumDurationFromArray(ComponentCreationTimeSumPerThread).Seconds() / float64(cdqCreationSuccessCount)
	}
	logData.AverageTimeToCreateComponents = averageTimeToCreateComponents

	logData.MaxTimeToCreateComponents = maxDurationFromArray(ComponentCreationTimeMaxPerThread).Seconds()

	componentCreationFailureRate := float64(componentCreationFailureCount) / float64(overallCount)
	logData.ComponentCreationFailureRate = componentCreationFailureRate

	// Compile data about PipelineRuns
	pipelineRunSuccessCount := sumFromArray(SuccessfulPipelineRunsPerThread)
	logData.PipelineRunSuccessCount = pipelineRunSuccessCount

	pipelineRunFailureCount := sumFromArray(FailedPipelineRunsPerThread)
	logData.PipelineRunFailureCount = pipelineRunFailureCount

	averageTimeToRunPipelineSucceeded := float64(0)
	if pipelineRunSuccessCount > 0 {
		averageTimeToRunPipelineSucceeded = sumDurationFromArray(PipelineRunSucceededTimeSumPerThread).Seconds() / float64(pipelineRunSuccessCount)
	}
	logData.AverageTimeToRunPipelineSucceeded = averageTimeToRunPipelineSucceeded

	logData.MaxTimeToRunPipelineSucceeded = maxDurationFromArray(PipelineRunSucceededTimeMaxPerThread).Seconds()

	averageTimeToRunPipelineFailed := float64(0)
	if pipelineRunFailureCount > 0 {
		averageTimeToRunPipelineFailed = sumDurationFromArray(PipelineRunFailedTimeSumPerThread).Seconds() / float64(pipelineRunFailureCount)
	}
	logData.AverageTimeToRunPipelineFailed = averageTimeToRunPipelineFailed

	pipelineRunFailureRate := float64(pipelineRunFailureCount) / float64(overallCount)
	logData.PipelineRunFailureRate = pipelineRunFailureRate

	// Compile data about integration tests
	integrationTestsPipelineRunSuccessCount := sumFromArray(SuccessfulIntegrationTestsPipelineRunsPerThread)
	logData.IntegrationTestsPipelineRunSuccessCount = integrationTestsPipelineRunSuccessCount

	integrationTestsPipelineRunFailureCount := sumFromArray(FailedIntegrationTestsPipelineRunsPerThread)
	logData.IntegrationTestsPipelineRunFailureCount = integrationTestsPipelineRunFailureCount

	IntegrationTestsAverageTimeToRunPipelineSucceeded := float64(0)
	if integrationTestsPipelineRunSuccessCount > 0 {
		IntegrationTestsAverageTimeToRunPipelineSucceeded = sumDurationFromArray(IntegrationTestsPipelineRunSucceededTimeSumPerThread).Seconds() / float64(integrationTestsPipelineRunSuccessCount)
	}
	logData.IntegrationTestsAverageTimeToRunPipelineSucceeded = IntegrationTestsAverageTimeToRunPipelineSucceeded

	logData.IntegrationTestsMaxTimeToRunPipelineSucceeded = maxDurationFromArray(IntegrationTestsPipelineRunSucceededTimeMaxPerThread).Seconds()

	IntegrationTestsAverageTimeToRunPipelineFailed := float64(0)
	if integrationTestsPipelineRunFailureCount > 0 {
		IntegrationTestsAverageTimeToRunPipelineFailed = sumDurationFromArray(IntegrationTestsPipelineRunFailedTimeSumPerThread).Seconds() / float64(integrationTestsPipelineRunFailureCount)
	}
	logData.IntegrationTestsAverageTimeToRunPipelineFailed = IntegrationTestsAverageTimeToRunPipelineFailed

	IntegrationTestsPipelineRunFailureRate := float64(integrationTestsPipelineRunFailureCount) / float64(overallCount)
	logData.IntegrationTestsPipelineRunFailureRate = IntegrationTestsPipelineRunFailureRate

	// Compile data about Deployments
	deploymentSuccessCount := sumFromArray(SuccessfulDeploymentsPerThread)
	logData.DeploymentSuccessCount = deploymentSuccessCount

	deploymentFailureCount := sumFromArray(FailedDeploymentsPerThread)
	logData.DeploymentFailureCount = deploymentFailureCount

	averageTimeToDeploymentSucceeded := float64(0)
	if deploymentSuccessCount > 0 {
		averageTimeToDeploymentSucceeded = sumDurationFromArray(DeploymentSucceededTimeSumPerThread).Seconds() / float64(deploymentSuccessCount)
	}
	logData.AverageTimeToDeploymentSucceeded = averageTimeToDeploymentSucceeded

	logData.MaxTimeToDeploymentSucceeded = maxDurationFromArray(DeploymentSucceededTimeMaxPerThread).Seconds()

	averageTimeToDeploymentFailed := float64(0)
	if deploymentFailureCount > 0 {
		averageTimeToDeploymentFailed = sumDurationFromArray(DeploymentFailedTimeSumPerThread).Seconds() / float64(deploymentFailureCount)
	}
	logData.AverageTimeToDeploymentFailed = averageTimeToDeploymentFailed

	deploymentFailureRate := float64(deploymentFailureCount) / float64(overallCount)
	logData.DeploymentFailureRate = deploymentFailureRate

	workloadKPI := logData.AverageTimeToCreateApplications + logData.AverageTimeToCreateCDQs + logData.AverageTimeToCreateComponents + logData.AverageTimeToRunPipelineSucceeded + logData.AverageTimeToDeploymentSucceeded
	logData.WorkloadKPI = workloadKPI
	if stage {
		StageCleanup(selectedUsers)
	}

	klog.Infof("🏁 Load Test Completed!")
	klog.Infof("📈 Results 📉")

	klog.Infof("Workload KPI: %.2f", workloadKPI)

	klog.Infof("Avg/max time to spin up users: %.2f s/%.2f s", averageTimeToSpinUpUsers, logData.MaxTimeToSpinUpUsers)
	klog.Infof("Avg/max time to create application: %.2f s/%.2f s", averageTimeToCreateApplications, logData.MaxTimeToCreateApplications)
	klog.Infof("Avg/max time to create integration test: %.2f s/%.2f s", averageTimeToCreateIts, logData.MaxTimeToCreateIts)
	klog.Infof("Avg/max time to create cdq: %.2f s/%.2f s", averageTimeToCreateCDQs, logData.MaxTimeToCreateCDQs)
	klog.Infof("Avg/max time to create component: %.2f s/%.2f s", averageTimeToCreateComponents, logData.MaxTimeToCreateComponents)
	klog.Infof("Avg/max time to complete pipelinesrun: %.2f s/%.2f s", averageTimeToRunPipelineSucceeded, logData.MaxTimeToRunPipelineSucceeded)
	klog.Infof("Avg/max time to complete integration test: %.2f s/%.2f s", IntegrationTestsAverageTimeToRunPipelineSucceeded, logData.IntegrationTestsMaxTimeToRunPipelineSucceeded)
	klog.Infof("Avg/max time to complete deployment: %.2f s/%.2f s", averageTimeToDeploymentSucceeded, logData.MaxTimeToDeploymentSucceeded)
	klog.Infof("Avg time to provision PVC : %.2f s", averageWaitTimeForPVCProvisioning)

	klog.Infof("Average time to fail pipelinerun: %.2f s", averageTimeToRunPipelineFailed)
	klog.Infof("Average time to fail integration test: %.2f s", IntegrationTestsAverageTimeToRunPipelineFailed)
	klog.Infof("Average time to fail deployment: %.2f s", averageTimeToDeploymentFailed)

	klog.Infof("Number of times user creation worked/failed: %d/%d (%.2f %%)", userCreationSuccessCount, userCreationFailureCount, userCreationFailureRate*100)
	klog.Infof("Number of times application creation worked/failed: %d/%d (%.2f %%)", applicationCreationSuccessCount, applicationCreationFailureCount, applicationCreationFailureRate*100)
	klog.Infof("Number of times integration tests creation worked/failed: %d/%d (%.2f %%)", itsCreationSuccessCount, itsCreationFailureCount, itsCreationFailureRate*100)

	klog.Infof("Number of times cdq creation worked/failed: %d/%d (%.2f %%)", cdqCreationSuccessCount, cdqCreationFailureCount, cdqCreationFailureRate*100)
	klog.Infof("Number of times component creation worked/failed: %d/%d (%.2f %%)", componentCreationSuccessCount, componentCreationFailureCount, componentCreationFailureRate*100)
	klog.Infof("Number of times pipeline run worked/failed: %d/%d (%.2f %%)", pipelineRunSuccessCount, pipelineRunFailureCount, pipelineRunFailureRate*100)
	klog.Infof("Number of times integration tests' pipeline run worked/failed: %d/%d (%.2f %%)", integrationTestsPipelineRunSuccessCount, integrationTestsPipelineRunFailureCount, IntegrationTestsPipelineRunFailureRate*100)
	klog.Infof("Number of times deployment worked/failed: %d/%d (%.2f %%)", deploymentSuccessCount, deploymentFailureCount, deploymentFailureRate*100)

	klog.Infoln("Error summary:")
	for _, errorCount := range errorCountMap {
		klog.Infof("Number of error #%d occured: %d", errorCount.ErrorCode, errorCount.Count)
		logData.ErrorCounts = append(logData.ErrorCounts, errorCount)
	}
	logData.ErrorsTotal = len(logData.Errors)
	klog.Infof("Total number of errors occured: %d", logData.ErrorsTotal)

	err = createLogDataJSON(fmt.Sprintf("%s/load-tests.json", outputDir), logData)
	if err != nil {
		klog.Errorf("error while marshalling JSON: %v\n", err)
	}

	klog.StopFlushDaemon()
	klog.Flush()
}

func StageCleanup(users []loadtestUtils.User) {

	for _, user := range users {
		framework := frameworkForUser(user.Username)
		err := framework.AsKubeDeveloper.HasController.DeleteAllApplicationsInASpecificNamespace(framework.UserNamespace, 5*time.Minute)
		if err != nil {
			klog.Errorf("while deleting resources for user: %s, got error: %v\n", user.Username, err)
		}

		err = framework.AsKubeDeveloper.HasController.DeleteAllComponentDetectionQueriesInASpecificNamespace(framework.UserNamespace, 5*time.Minute)
		if err != nil {
			klog.Errorf("while deleting component detection queries for user: %s, got error: %v\n", user.Username, err)
		}
	}
}

func MetricsWrapper(M *metrics.MetricsPush, collector string, metricType string, metric string, values ...float64) {
	if !disableMetrics {
		M.PushMetrics(collector, metricType, metric, values...)
	}
}

func maxDurationFromArray(durations []time.Duration) time.Duration {
	max := time.Duration(0)
	for _, i := range durations {
		if i > max {
			max = i
		}
	}
	return max
}

func sumDurationFromArray(durations []time.Duration) time.Duration {
	sum := time.Duration(0)
	for _, i := range durations {
		sum += i
	}
	return sum
}

func sumFromArray(array []int64) int64 {
	sum := int64(0)
	for _, i := range array {
		sum += i
	}
	return sum
}

func increaseBar(bar *uiprogress.Bar, mutex *sync.Mutex) {
	if enableProgressBars {
		mutex.Lock()
		defer mutex.Unlock()
		bar.Incr()
	}
}

func componentForUser(username string) string {
	val, ok := userComponentMap.Load(username)
	if ok {
		componentName, ok2 := val.(string)
		if ok2 {
			return componentName
		} else {
			klog.Errorf("Invalid type of map value: %+v", val)
		}
	}
	return ""
}

func frameworkForUser(username string) *framework.Framework {
	val, ok := frameworkMap.Load(username)
	if ok {
		framework, ok2 := val.(*framework.Framework)
		if ok2 {
			return framework
		} else {
			klog.Errorf("Invalid type of map value: %+v", val)
		}
	}
	return nil
}

func testScenarioForUser(username string) string {
	val, ok := userTestScenarioMap.Load(username)
	if ok {
		testScenarioName, ok2 := val.(string)
		if ok2 {
			return testScenarioName
		} else {
			klog.Errorf("Invalid type of map value: %+v", val)
		}
	}
	return ""
}

func userComponentPipelineRunForUser(username string) string {
	val, ok := userComponentPipelineRunMap.Load(username)
	if ok {
		componentPipelineRunName, ok2 := val.(string)
		if ok2 {
			return componentPipelineRunName
		} else {
			klog.Errorf("Invalid type of map value: %+v", val)
		}
	}
	return ""
}

func tryNewFramework(username string, user loadtestUtils.User, timeout time.Duration) (*framework.Framework, error) {
	ch := make(chan *framework.Framework)
	var fw *framework.Framework
	var err error

	// Debug
	klog.Infof("** Stage username - %s; ToolchainApiUrl - %s; KeycloakUrl - %s; OfflineToken - %s; user - %v; timeout - %f...\n", username, user.APIURL, user.SSOURL, user.Token, user, timeout)

	go func() {
		if stage {
			fw, err = framework.NewFrameworkWithTimeout(
				user.Username,
				time.Minute*60,
				utils.Options{
					ToolchainApiUrl: user.APIURL,
					KeycloakUrl:     user.SSOURL,
					OfflineToken:    user.Token,
				})
		} else {
			fw, err = framework.NewFrameworkWithTimeout(username, time.Minute*60)
		}
		ch <- fw
	}()

	var ret *framework.Framework

	select {
	case result := <-ch:
		ret = result
	case <-time.After(timeout):
		ret = nil
		err = fmt.Errorf("unable to create new framework for user %s within %v", username, timeout)
	}

	return ret, err
}

func userJourneyThread(frameworkMap *sync.Map, threadWaitGroup *sync.WaitGroup, threadIndex int, usersBar *uiprogress.Bar, applicationsBar *uiprogress.Bar, itsBar *uiprogress.Bar, cdqsBar *uiprogress.Bar, componentsBar *uiprogress.Bar, pipelinesBar *uiprogress.Bar, integrationTestsPipelinesBar *uiprogress.Bar, deploymentsBar *uiprogress.Bar) {
	chUsers := make(chan string, numberOfUsers)
	chPipelines := make(chan string, numberOfUsers)
	chIntegrationTestsPipelines := make(chan string, numberOfUsers)
	chDeployments := make(chan string, numberOfUsers)

	defer threadWaitGroup.Done()
	var wg *sync.WaitGroup = &sync.WaitGroup{}

	if waitPipelines {
		if waitIntegrationTestsPipelines {
			if waitDeployments {
				wg.Add(5)
			} else {
				wg.Add(4)
			}
		} else {
			wg.Add(3)
		}
	} else {
		wg.Add(2)
	}

	// reduce the nested if blocks - SonarCloud suggestion
	/*
		addValue := 2
		if waitPipelines {
			addValue = 3
			if waitIntegrationTestsPipelines {
				addValue = 4
				if waitDeployments {
					addValue = 5
				}
			}
		}
		wg.Add(addValue)
	*/

	go func() {
		defer wg.Done()
		for userIndex := 1; userIndex <= numberOfUsers; userIndex++ {
			startTime := time.Now()

			var username string
			if randomString {
				// Create a 5 characters wide random string to be added to username (https://issues.redhat.com/browse/RHTAP-1338)
				randomStr := randomStringFromCharset(5)
				username = fmt.Sprintf("%s-%s-%04d", usernamePrefix, randomStr, threadIndex*numberOfUsers+userIndex)
			} else {
				username = fmt.Sprintf("%s-%04d", usernamePrefix, threadIndex*numberOfUsers+userIndex)
			}

			var user loadtestUtils.User
			var framework *framework.Framework
			var err error
			if stage {
				user = selectedUsers[threadIndex*numberOfUsers+userIndex-1]
				username = user.Username
				framework, err = tryNewFramework(username, user, 60*time.Minute)
			} else {
				framework, err = tryNewFramework(username, user, 60*time.Minute)
			}
			if err != nil {
				logError(1, fmt.Sprintf("Unable to provision user '%s': %v", username, err))
				FailedUserCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorUsers, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedUserCreationsCounter)
				increaseBar(usersBar, usersBarMutex)
				continue
			} else {
				frameworkMap.Store(username, framework)
			}

			chUsers <- username

			userCreationTime := time.Since(startTime)
			UserCreationTimeSumPerThread[threadIndex] += userCreationTime
			MetricsWrapper(MetricsController, metricsConstants.CollectorUsers, metricsConstants.MetricTypeGuage, metricsConstants.MetricUserCreationTimeGauge, float64(userCreationTime))
			if userCreationTime > UserCreationTimeMaxPerThread[threadIndex] {
				UserCreationTimeMaxPerThread[threadIndex] = userCreationTime
			}

			SuccessfulUserCreationsPerThread[threadIndex] += 1
			MetricsWrapper(MetricsController, metricsConstants.CollectorUsers, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulUserCreationsCounter)
			increaseBar(usersBar, usersBarMutex)
		}
		close(chUsers)
	}()

	go func() {
		defer wg.Done()
		for username := range chUsers {
			framework := frameworkForUser(username)
			if framework == nil {
				logError(2, fmt.Sprintf("Framework not found for username %s", username))
				continue
			}
			usernamespace := framework.UserNamespace

			ApplicationName := fmt.Sprintf("%s-app", username)
			startTimeForApplication := time.Now()

			//Trace CreateApplicationWithTimeout call
			params := map[string]interface{}{
				"BeforeOrAfter":   "Before",
				"ApplicationName": ApplicationName,
				"username":        username,
				"usernamespace":   usernamespace,
				"timeout":         60 * time.Minute,
			}
			TraceFunction("CreateApplicationWithTimeout", params)

			app, err := framework.AsKubeDeveloper.HasController.CreateApplicationWithTimeout(ApplicationName, usernamespace, 60*time.Minute)

			//Trace CreateApplicationWithTimeout call
			params = map[string]interface{}{
				"BeforeOrAfter":   "After",
				"ApplicationName": ApplicationName,
				"username":        username,
				"usernamespace":   usernamespace,
				"timeout":         60 * time.Minute,
			}
			TraceFunction("CreateApplicationWithTimeout", params)

			applicationCreationTime := time.Since(startTimeForApplication)
			if err != nil {
				logError(3, fmt.Sprintf("Unable to create the Application %s: %v", ApplicationName, err))
				FailedApplicationCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedApplicationCreationCounter)
				increaseBar(applicationsBar, applicationsBarMutex)
				continue
			}

			ApplicationCreationTimeSumPerThread[threadIndex] += applicationCreationTime
			MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeGuage, metricsConstants.MetricApplicationCreationTimeGauge, applicationCreationTime.Seconds())
			if applicationCreationTime > ApplicationCreationTimeMaxPerThread[threadIndex] {
				ApplicationCreationTimeMaxPerThread[threadIndex] = applicationCreationTime
			}

			for _, Status := range app.Status.Conditions {
				if Status.Type == "Created" {
					TempTime := Status.LastTransitionTime.Sub(startTimeForApplication)
					MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualApplicationCreationTimeGauge, TempTime.Seconds())
				}

			}
			SuccessfulApplicationCreationsPerThread[threadIndex] += 1
			MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulApplicationCreationCounter)

			increaseBar(applicationsBar, applicationsBarMutex)

			/*
				This part adds the Integration test scenario part
				It's considered also as part of the resources creation since Integration test scenarios are resources as well
			*/
			var integrationTestScenario *integrationv1beta1.IntegrationTestScenario

			//Trace CreateIntegrationTestScenario_beta1 call
			params = map[string]interface{}{
				"BeforeOrAfter":          "Before",
				"ApplicationName":        ApplicationName,
				"username":               username,
				"usernamespace":          usernamespace,
				"testScenarioGitURL":     testScenarioGitURL,
				"testScenarioRevision":   testScenarioRevision,
				"testScenarioPathInRepo": testScenarioPathInRepo,
			}
			TraceFunction("CreateIntegrationTestScenario", params)

			startTimeForIts := time.Now()
			integrationTestScenario, err = framework.AsKubeDeveloper.IntegrationController.CreateIntegrationTestScenario_beta1(ApplicationName, usernamespace, testScenarioGitURL, testScenarioRevision, testScenarioPathInRepo)
			itsCreationTime := time.Since(startTimeForIts)
			//Trace CreateIntegrationTestScenario_beta1 call
			params = map[string]interface{}{
				"BeforeOrAfter":          "After",
				"ApplicationName":        ApplicationName,
				"username":               username,
				"usernamespace":          usernamespace,
				"testScenarioGitURL":     testScenarioGitURL,
				"testScenarioRevision":   testScenarioRevision,
				"testScenarioPathInRepo": testScenarioPathInRepo,
			}
			TraceFunction("CreateIntegrationTestScenario", params)

			if err != nil {
				logError(51, fmt.Sprintf("Unable to create integrationTestScenario for Application %s: %v \n", ApplicationName, err))
				FailedItsCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationTestSenarioCreationCounter)
				increaseBar(itsBar, itsBarMutex)
				continue
			}

			itsName := integrationTestScenario.Name

			ItsCreationTimeSumPerThread[threadIndex] += itsCreationTime
			MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeGuage, metricsConstants.MetricIntegrationTestSenarioCreationTimeGauge, itsCreationTime.Seconds())
			if itsCreationTime > ItsCreationTimeMaxPerThread[threadIndex] {
				ItsCreationTimeMaxPerThread[threadIndex] = itsCreationTime
			}

			// Wait for
			// 1. our its to be ready (condition.Type == "IntegrationTestScenarioValid" && condition.Status == "True")
			// or
			// 2. our its to be in error (condition.Status == "True" && (strings.HasPrefix(condition.Type, "Error") || strings.HasSuffix(condition.Type, "Error"))
			integrationTestScenarioRepoInterval := 10 * time.Second
			integrationTestScenarioValidationTimeout := time.Minute * 30

			var conditionError error

			if err = utils.WaitUntilWithInterval(func() (done bool, err error) {
				integrationTestScenarios, err := framework.AsKubeDeveloper.IntegrationController.GetIntegrationTestScenarios(ApplicationName, usernamespace)
				if err != nil {
					// Return an error immediately if we cannot fetch the scenarios
					return false, fmt.Errorf("unable to get created integrationTestScenario %s for Application %s: %v", itsName, ApplicationName, err)
				}

				conditionError = nil // Reset the condition error
				for _, testScenario := range *integrationTestScenarios {
					if testScenario.Name == itsName {
						if len(testScenario.Status.Conditions) == 0 {
							// store the error in conditionError
							conditionError = fmt.Errorf("integrationTestScenario %s has 0 status conditions", testScenario.Name)
							return false, nil // Continue polling
						}
						creationTimestamp := testScenario.ObjectMeta.CreationTimestamp.Time

						for _, condition := range testScenario.Status.Conditions {
							if condition.Type == "IntegrationTestScenarioValid" && condition.Status == "True" {
								lastTransitionTime := condition.LastTransitionTime.Time
								// itsActualCreationTime := lastTransitionTime.Sub(startTimeForIts)
								// MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualIntegrationTestSenarioCreationTimeGauge, itsActualCreationTime.Seconds())

								// Since we have a skewed time between the clocks of the test machine and the cluster,
								//  we shall calculate itsActualCreationTime as below:
								itsActualCreationTimeInSeconds := itsCreationTime.Seconds() + lastTransitionTime.Sub(creationTimestamp).Seconds()

								MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualIntegrationTestSenarioCreationTimeGauge, itsActualCreationTimeInSeconds)
								SuccessfulItsCreationsPerThread[threadIndex] += 1
								MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulIntegrationTestSenarioCreationCounter)

								increaseBar(itsBar, itsBarMutex)
								userTestScenarioMap.Store(username, itsName)
								return true, nil
							}

							// Error indicators for various CRs besides snapshotenvironmentbindings.json
							// .status.conditions array has a .status of "True" and a .type that ends or starts with "Error"
							if condition.Status == "True" && (strings.HasPrefix(condition.Type, "Error") || strings.HasSuffix(condition.Type, "Error")) {
								return true, fmt.Errorf("integrationTestScenario %s is in Error state: %s", testScenario.Name, condition.Message)
							}
						}
					}
				}

				return false, nil // Indicate to continue polling
			}, integrationTestScenarioRepoInterval, integrationTestScenarioValidationTimeout); err != nil {
				// If an error is returned, handle it immediately
				logError(52, fmt.Sprintf("Failed to validate integrationTestScenario for Application %s due to an error: %v \n", ApplicationName, err))
				FailedItsCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationTestSenarioCreationCounter)
				increaseBar(itsBar, itsBarMutex)
			} else if conditionError != nil {
				// If no error is returned but conditionError is set, it means the timeout has occurred
				logError(52, fmt.Sprintf("Failed to validate integrationTestScenario for Application %s due to an error: %v \n", ApplicationName, conditionError.Error()))
				FailedItsCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationTestSenarioCreationCounter)
				increaseBar(itsBar, itsBarMutex)
			}

			ComponentDetectionQueryName := fmt.Sprintf("%s-cdq", username)
			startTimeForCDQ := time.Now()

			//Trace CreateComponentDetectionQueryWithTimeout call
			params = map[string]interface{}{
				"BeforeOrAfter":               "Before",
				"ApplicationName":             ApplicationName,
				"username":                    username,
				"usernamespace":               usernamespace,
				"ComponentDetectionQueryName": ComponentDetectionQueryName,
				"componentRepoUrl":            testScenarioRevision,
				"timeout":                     60 * time.Minute,
			}
			TraceFunction("CreateComponentDetectionQuery", params)

			cdq, err := framework.AsKubeDeveloper.HasController.CreateComponentDetectionQueryWithTimeout(ComponentDetectionQueryName, usernamespace, componentRepoUrl, "", "", "", false, 60*time.Minute)

			//Trace CreateComponentDetectionQueryWithTimeout call
			params = map[string]interface{}{
				"BeforeOrAfter":               "After",
				"ApplicationName":             ApplicationName,
				"username":                    username,
				"usernamespace":               usernamespace,
				"ComponentDetectionQueryName": ComponentDetectionQueryName,
				"componentRepoUrl":            testScenarioRevision,
				"timeout":                     60 * time.Minute,
			}
			TraceFunction("CreateComponentDetectionQuery", params)

			cdqCreationTime := time.Since(startTimeForCDQ)

			if err != nil {
				logError(6, fmt.Sprintf("Unable to create ComponentDetectionQuery %s: %v", ComponentDetectionQueryName, err))
				FailedCDQCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
				increaseBar(cdqsBar, cdqsBarMutex)
				continue
			}
			if cdq.Name != ComponentDetectionQueryName {
				logError(7, fmt.Sprintf("Actual cdq name (%s) does not match expected (%s): %v", cdq.Name, ComponentDetectionQueryName, err))
				FailedCDQCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
				increaseBar(cdqsBar, cdqsBarMutex)
				continue
			}
			if len(cdq.Status.ComponentDetected) > 1 {
				logError(8, fmt.Sprintf("cdq (%s) detected more than 1 component", cdq.Name))
				FailedCDQCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
				increaseBar(cdqsBar, cdqsBarMutex)
				continue
			}

			CDQCreationTimeSumPerThread[threadIndex] += cdqCreationTime
			MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeGuage, metricsConstants.MetricCDQCreationTimeGauge, cdqCreationTime.Seconds())
			if cdqCreationTime > CDQCreationTimeMaxPerThread[threadIndex] {
				CDQCreationTimeMaxPerThread[threadIndex] = cdqCreationTime
			}
			SuccessfulCDQCreationsPerThread[threadIndex] += 1
			MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulCDQCreationCounter)

			for _, cdqInstance := range cdq.Status.Conditions {
				if cdqInstance.Type == "Completed" {
					TempTime := cdqInstance.LastTransitionTime.Sub(startTimeForCDQ)
					MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualCDQCreationTimeGauge, TempTime.Seconds())
				}
			}
			increaseBar(cdqsBar, cdqsBarMutex)

			for _, compStub := range cdq.Status.ComponentDetected {
				startTimeForComponent := time.Now()

				//Trace CreateComponent call
				params = map[string]interface{}{
					"BeforeOrAfter":             "Before",
					"ApplicationName":           ApplicationName,
					"username":                  username,
					"usernamespace":             usernamespace,
					"compStub.ComponentStub":    compStub.ComponentStub,
					"pipelineSkipInitialChecks": pipelineSkipInitialChecks,
				}
				TraceFunction("CreateComponent", params)

				component, err := framework.AsKubeDeveloper.HasController.CreateComponent(compStub.ComponentStub, usernamespace, "", "", ApplicationName, pipelineSkipInitialChecks, map[string]string{})

				//Trace CreateComponent call
				params = map[string]interface{}{
					"BeforeOrAfter":             "After",
					"ApplicationName":           ApplicationName,
					"username":                  username,
					"usernamespace":             usernamespace,
					"compStub.ComponentStub":    compStub.ComponentStub,
					"pipelineSkipInitialChecks": pipelineSkipInitialChecks,
				}
				TraceFunction("CreateComponent", params)

				componentCreationTime := time.Since(startTimeForComponent)

				if err != nil {
					logError(9, fmt.Sprintf("Unable to create the Component %s: %v", compStub.ComponentStub.ComponentName, err))
					FailedComponentCreationsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedComponentCreationCounter)
					increaseBar(componentsBar, componentsBarMutex)
					continue
				}
				if component.Name != compStub.ComponentStub.ComponentName {
					logError(10, fmt.Sprintf("Actual component name (%s) does not match expected (%s): %v", component.Name, compStub.ComponentStub.ComponentName, err))
					FailedComponentCreationsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedComponentCreationCounter)
					increaseBar(componentsBar, componentsBarMutex)
					continue
				}
				userComponentMap.Store(username, component.Name)

				ComponentCreationTimeSumPerThread[threadIndex] += componentCreationTime
				MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeGuage, metricsConstants.MetricComponentCreationTimeGauge, componentCreationTime.Seconds())
				if componentCreationTime > ComponentCreationTimeMaxPerThread[threadIndex] {
					ComponentCreationTimeMaxPerThread[threadIndex] = componentCreationTime
				}
				SuccessfulComponentCreationsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulComponentCreationCounter)

				for _, componentStatus := range component.Status.Conditions {
					if componentStatus.Type == "Created" {
						TempTime := componentStatus.LastTransitionTime.Sub(startTimeForComponent)
						MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualComponentCreationTimeGauge, TempTime.Seconds())

					}
				}

				increaseBar(componentsBar, componentsBarMutex)
			}

			chPipelines <- username
		}
		close(chPipelines)
	}()

	if waitPipelines {
		go func() {
			defer wg.Done()
			for username := range chPipelines {
				framework := frameworkForUser(username)
				if framework == nil {
					logError(11, fmt.Sprintf("Framework not found for username %s", username))
					increaseBar(pipelinesBar, pipelinesBarMutex)
					continue
				}
				usernamespace := framework.UserNamespace
				componentName := componentForUser(username)
				if componentName == "" {
					logError(12, fmt.Sprintf("Component not found for username %s", username))
					increaseBar(pipelinesBar, pipelinesBarMutex)
					continue
				}
				applicationName := fmt.Sprintf("%s-app", username)
				pipelineCreatedRetryInterval := time.Second * 5
				pipelineCreatedTimeout := time.Minute * 15
				var pipelineRun *v1beta1.PipelineRun
				err := k8swait.PollUntilContextTimeout(context.Background(), pipelineCreatedRetryInterval, pipelineCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {

					//Trace GetComponentPipelineRun call
					params := map[string]interface{}{
						"BeforeOrAfter":            "Before",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"componentPipelineRunName": "",
					}
					TraceFunction("GetComponentPipelineRun", params)

					// Searching for "build" type of pipelineRun
					pipelineRun, err = framework.AsKubeDeveloper.HasController.GetComponentPipelineRunWithType(componentName, applicationName, usernamespace, "build", "")

					//Trace GetComponentPipelineRun call
					params = map[string]interface{}{
						"BeforeOrAfter":            "After",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"componentPipelineRunName": pipelineRun.Name,
					}
					TraceFunction("GetComponentPipelineRun", params)

					if err != nil {
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						return false, nil
					}
					return true, nil
				})
				if err != nil {
					logError(13, fmt.Sprintf("PipelineRun for applicationName/componentName %s/%s has not been created within %v: %v", applicationName, componentName, pipelineCreatedTimeout, err))
					FailedPipelineRunsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedPipelineRunsCreationCounter)

					increaseBar(pipelinesBar, pipelinesBarMutex)
					continue
				}
				userComponentPipelineRunMap.Store(username, pipelineRun.Name)

				// Debug
				klog.Infof("build-pipelinerun from GetComponentPipelineRun - %s, pipelineRunName fetched from userComponentPipelineRunMap - %s", pipelineRun.Name, userComponentPipelineRunForUser(username))
				// Debug

				pipelineRunRetryInterval := time.Second * 5
				pipelineRunTimeout := time.Minute * 60
				err = k8swait.PollUntilContextTimeout(context.Background(), pipelineRunRetryInterval, pipelineRunTimeout, false, func(ctx context.Context) (done bool, err error) {

					//Trace GetComponentPipelineRun call
					params := map[string]interface{}{
						"BeforeOrAfter":            "Before",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"componentPipelineRunName": pipelineRun.Name,
					}
					TraceFunction("GetComponentPipelineRun", params)

					pipelineRun, err = framework.AsKubeDeveloper.HasController.GetComponentPipelineRunWithType(componentName, applicationName, usernamespace, "build", "")

					//Trace GetComponentPipelineRun call
					params = map[string]interface{}{
						"BeforeOrAfter":            "After",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"componentPipelineRunName": pipelineRun.Name,
					}
					TraceFunction("GetComponentPipelineRun", params)

					if err != nil {
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						return false, nil
					}
					if pipelineRun.IsDone() {
						succeededCondition := pipelineRun.Status.GetCondition(apis.ConditionSucceeded)
						pvcs, err := framework.AsKubeAdmin.TektonController.KubeInterface().CoreV1().PersistentVolumeClaims(pipelineRun.Namespace).List(context.Background(), metav1.ListOptions{})
							if err != nil {
								logError(23, fmt.Sprintf("Error getting PVC: %v\n", err))
							}
							for _, pvc := range pvcs.Items {
								pv, err := framework.AsKubeAdmin.TektonController.KubeInterface().CoreV1().PersistentVolumes().Get(context.Background(), pvc.Spec.VolumeName, metav1.GetOptions{})
								if err != nil {
									logError(24, fmt.Sprintf("Error getting PV: %v\n", err))
									continue
								}
								waittime := (pv.ObjectMeta.CreationTimestamp.Time).Sub(pvc.ObjectMeta.CreationTimestamp.Time)
								PipelineRunWaitTimeForPVCSumPerThread[threadIndex] += waittime
								SuccessfulPVCCreationsPerThread[threadIndex] += 1
							}
						if succeededCondition.IsFalse() {
							dur := pipelineRun.Status.CompletionTime.Sub(pipelineRun.CreationTimestamp.Time)
							PipelineRunFailedTimeSumPerThread[threadIndex] += dur
							logError(14, fmt.Sprintf("Pipeline run for applicationName/componentName %s/%s failed due to %v: %v", applicationName, componentName, succeededCondition.Reason, succeededCondition.Message))
							FailedPipelineRunsPerThread[threadIndex] += 1
							MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedPipelineRunsCreationCounter)
						} else {
							dur := pipelineRun.Status.CompletionTime.Sub(pipelineRun.CreationTimestamp.Time)
							PipelineRunSucceededTimeSumPerThread[threadIndex] += dur
							MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeGuage, metricsConstants.MetricPipelineRunsTimeGauge, dur.Seconds())
							if dur > PipelineRunSucceededTimeMaxPerThread[threadIndex] {
								PipelineRunSucceededTimeMaxPerThread[threadIndex] = dur
							}
							SuccessfulPipelineRunsPerThread[threadIndex] += 1
							MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulPipelineRunsCreationCounter)

							chIntegrationTestsPipelines <- username
						}
						increaseBar(pipelinesBar, pipelinesBarMutex)
					}
					return pipelineRun.IsDone(), nil
				})
				if err != nil {
					logError(15, fmt.Sprintf("Pipeline run for applicationName/componentName %s/%s failed to succeed within %v: %v", applicationName, componentName, pipelineRunTimeout, err))
					FailedPipelineRunsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedPipelineRunsCreationCounter)
					increaseBar(pipelinesBar, pipelinesBarMutex)
					continue
				}
			}
			close(chIntegrationTestsPipelines)
		}()
	}

	if waitIntegrationTestsPipelines {
		go func() {
			defer wg.Done()
			for username := range chIntegrationTestsPipelines {
				// since username added to chIntegrationTestsPipelinesents only after valid framework, usernamespace, componentName, testScenarioName,
				//  componentPipelineRunName, and applicationName have been created
				//  we don't need to verify validity for neither
				framework := frameworkForUser(username)
				usernamespace := framework.UserNamespace
				componentName := componentForUser(username)
				testScenarioName := testScenarioForUser(username)
				componentPipelineRunName := userComponentPipelineRunForUser(username)
				applicationName := fmt.Sprintf("%s-app", username)
				SnapshotCreatedRetryInterval := time.Second * 5
				SnapshotCreatedTimeout := time.Minute * 30
				IntegrationTestsPipelineCreatedRetryInterval := time.Second * 5
				IntegrationTestsPipelineCreatedTimeout := time.Minute * 15

				var snapshot *appstudioApi.Snapshot
				err := k8swait.PollUntilContextTimeout(context.Background(), SnapshotCreatedRetryInterval, SnapshotCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {

					//Trace GetSnapshot call
					params := map[string]interface{}{
						"BeforeOrAfter":            "Before",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"testScenarioName":         testScenarioName,
						"componentPipelineRunName": componentPipelineRunName,
					}
					TraceFunction("GetSnapshot", params)

					snapshot, err = framework.AsKubeDeveloper.IntegrationController.GetSnapshot("", componentPipelineRunName, "", usernamespace)

					//Trace GetSnapshot call
					params = map[string]interface{}{
						"BeforeOrAfter":            "After",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"testScenarioName":         testScenarioName,
						"componentPipelineRunName": componentPipelineRunName,
					}
					TraceFunction("GetSnapshot", params)

					if err != nil {
						klog.Infof("Unable getting snapshot - %v", err)
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						return false, nil
					}
					return true, nil
				})
				if err != nil {
					logError(16, fmt.Sprintf("Snapshot for applicationName/componentName %s/%s has not been created within %v: %v", applicationName, componentName, SnapshotCreatedTimeout, err))
					FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
					increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
					continue
				}

				var IntegrationTestsPipelineRun *v1beta1.PipelineRun
				err = k8swait.PollUntilContextTimeout(context.Background(), IntegrationTestsPipelineCreatedRetryInterval, IntegrationTestsPipelineCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {

					//Trace GetIntegrationPipelineRun call
					params := map[string]interface{}{
						"BeforeOrAfter":            "Before",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"testScenarioName":         testScenarioName,
						"componentPipelineRunName": componentPipelineRunName,
						"snapshotName":             snapshot.Name,
					}
					TraceFunction("GetIntegrationPipelineRun", params)

					IntegrationTestsPipelineRun, err = framework.AsKubeDeveloper.IntegrationController.GetIntegrationPipelineRun(testScenarioName, snapshot.Name, usernamespace)

					//Trace GetIntegrationPipelineRun call
					params = map[string]interface{}{
						"BeforeOrAfter":            "After",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"testScenarioName":         testScenarioName,
						"componentPipelineRunName": componentPipelineRunName,
						"snapshotName":             snapshot.Name,
					}
					TraceFunction("GetIntegrationPipelineRun", params)

					if err != nil {
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						return false, nil
					}
					return true, nil
				})
				if err != nil {
					logError(17, fmt.Sprintf("IntegrationTestPipelineRun for applicationName/testScenarioName/snapshotName %s/%s/%s has not been created within %v: %v", applicationName, testScenarioName, snapshot.Name, IntegrationTestsPipelineCreatedTimeout, err))
					FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
					increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
					continue
				}
				IntegrationTestsPipelineRunRetryInterval := time.Second * 5
				IntegrationTestsPipelineRunTimeout := time.Minute * 60
				err = k8swait.PollUntilContextTimeout(context.Background(), IntegrationTestsPipelineRunRetryInterval, IntegrationTestsPipelineRunTimeout, false, func(ctx context.Context) (done bool, err error) {

					//Trace GetIntegrationPipelineRun call
					params := map[string]interface{}{
						"BeforeOrAfter":            "Before",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"testScenarioName":         testScenarioName,
						"componentPipelineRunName": componentPipelineRunName,
						"snapshotName":             snapshot.Name,
					}
					TraceFunction("GetIntegrationPipelineRun", params)

					IntegrationTestsPipelineRun, err = framework.AsKubeDeveloper.IntegrationController.GetIntegrationPipelineRun(testScenarioName, snapshot.Name, usernamespace)

					//Trace GetIntegrationPipelineRun call
					params = map[string]interface{}{
						"BeforeOrAfter":            "After",
						"ApplicationName":          applicationName,
						"username":                 username,
						"usernamespace":            usernamespace,
						"componentName":            componentName,
						"testScenarioName":         testScenarioName,
						"componentPipelineRunName": componentPipelineRunName,
						"snapshotName":             snapshot.Name,
					}
					TraceFunction("GetIntegrationPipelineRun", params)

					if err != nil {
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						return false, nil
					}
					if IntegrationTestsPipelineRun.IsDone() {
						succeededCondition := IntegrationTestsPipelineRun.Status.GetCondition(apis.ConditionSucceeded)
						if succeededCondition.IsFalse() {
							dur := IntegrationTestsPipelineRun.Status.CompletionTime.Sub(IntegrationTestsPipelineRun.CreationTimestamp.Time)
							IntegrationTestsPipelineRunFailedTimeSumPerThread[threadIndex] += dur
							logError(18, fmt.Sprintf("IntegrationTestPipelineRun for applicationName/testScenarioName/snapshotName %s/%s/%s failed due to %v: %v", applicationName, testScenarioName, snapshot.Name, succeededCondition.Reason, succeededCondition.Message))
							FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
							MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
						} else {
							dur := IntegrationTestsPipelineRun.Status.CompletionTime.Sub(IntegrationTestsPipelineRun.CreationTimestamp.Time)
							IntegrationTestsPipelineRunSucceededTimeSumPerThread[threadIndex] += dur
							MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeGuage, metricsConstants.MetricIntegrationPipelineRunsTimeGauge, dur.Seconds())
							if dur > IntegrationTestsPipelineRunSucceededTimeMaxPerThread[threadIndex] {
								IntegrationTestsPipelineRunSucceededTimeMaxPerThread[threadIndex] = dur
							}
							SuccessfulIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
							MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulIntegrationPipelineRunsCreationCounter)
							chDeployments <- username
						}
						increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
					}
					return IntegrationTestsPipelineRun.IsDone(), nil
				})
				if err != nil {
					logError(19, fmt.Sprintf("IntegrationTestPipelineRun for applicationName/testScenarioName/snapshotName %s/%s/%s failed to succeed within %v: %v", applicationName, testScenarioName, snapshot.Name, IntegrationTestsPipelineRunTimeout, err))
					FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
					increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
					continue
				}
			}
			close(chDeployments)
		}()
	}

	if waitDeployments {
		go func() {
			defer wg.Done()
			for username := range chDeployments {
				// since username added to chDeployments only after valid framework, usernamespace, componentName, and applicationName have been created
				//  we don't need to verify validity for neither
				framework := frameworkForUser(username)
				usernamespace := framework.UserNamespace
				componentName := componentForUser(username)
				applicationName := fmt.Sprintf("%s-app", username)
				deploymentCreatedRetryInterval := time.Second * 5
				deploymentCreatedTimeout := time.Minute * 30
				var deployment *appsv1.Deployment

				// Deploy the component using gitops and check for the health
				err := k8swait.PollUntilContextTimeout(context.Background(), deploymentCreatedRetryInterval, deploymentCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {

					//Trace GetDeployment call
					params := map[string]interface{}{
						"BeforeOrAfter":   "Before",
						"ApplicationName": applicationName,
						"username":        username,
						"usernamespace":   usernamespace,
						"componentName":   componentName,
					}
					TraceFunction("GetDeployment", params)

					deployment, err = framework.AsKubeDeveloper.CommonController.GetDeployment(componentName, usernamespace)

					//Trace GetDeployment call
					params = map[string]interface{}{
						"BeforeOrAfter":   "After",
						"ApplicationName": applicationName,
						"username":        username,
						"usernamespace":   usernamespace,
						"componentName":   componentName,
					}
					TraceFunction("GetDeployment", params)

					if err != nil {
						klog.Infof("Unable getting deployment - %v", err)
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						return false, nil
					}
					return true, nil
				})
				if err != nil {
					logError(20, fmt.Sprintf("Deployment for applicationName/componentName %s/%s has not been created within %v: %v", applicationName, componentName, deploymentCreatedTimeout, err))
					FailedDeploymentsPerThread[threadIndex] += 1
					increaseBar(deploymentsBar, deploymentsBarMutex)
					continue
				}

				deploymentRetryIntervalInitialValue := time.Second * 5
				deploymentRetryInterval := deploymentRetryIntervalInitialValue
				deploymentTimeout := time.Minute * 30
				var conditionError error
				var (
					deploymentFailed       bool
					errorMessage           string
					lastUpdateTimeOfFailed metav1.Time
					creationTimestamp      metav1.Time
				)

				// To avoid race conditions we want to check for deployment failure that occurs more than once to signify failure (deploymentFailCount > 3)
				err = k8swait.PollUntilContextTimeout(context.Background(), deploymentRetryInterval, deploymentTimeout, false, func(ctx context.Context) (done bool, err error) {

					conditionError = nil // Reset the condition error
					//Trace GetDeployment call
					params := map[string]interface{}{
						"BeforeOrAfter":   "Before",
						"ApplicationName": applicationName,
						"username":        username,
						"usernamespace":   usernamespace,
						"componentName":   componentName,
					}
					TraceFunction("GetDeployment", params)

					deployment, err = framework.AsKubeDeveloper.CommonController.GetDeployment(componentName, usernamespace)

					//Trace GetDeployment call
					params = map[string]interface{}{
						"BeforeOrAfter":   "After",
						"ApplicationName": applicationName,
						"username":        username,
						"usernamespace":   usernamespace,
						"componentName":   componentName,
					}
					TraceFunction("GetDeployment", params)

					if err != nil {
						time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
						conditionError = err
						return false, nil
					}

					creationTimestamp = deployment.ObjectMeta.CreationTimestamp
					deploymentIsDone, lastUpdateTimeOfDone := checkDeploymentIsDone(deployment)

					if !deploymentIsDone {
						deploymentFailed, errorMessage, lastUpdateTimeOfFailed = checkDeploymentFailed(deployment)
						if deploymentFailed {
							conditionError = fmt.Errorf("%s", errorMessage)
							// the deployment can succeed later so we don't return until success or timeout
							return false, nil
						}
					} else {
						dur := lastUpdateTimeOfDone.Time.Sub(creationTimestamp.Time)
						MetricsWrapper(MetricsController, metricsConstants.CollectorDeployments, metricsConstants.MetricTypeGuage, metricsConstants.MetricDeploymentsCreationTimeGauge, dur.Seconds())
						DeploymentSucceededTimeSumPerThread[threadIndex] += dur
						if dur > DeploymentSucceededTimeMaxPerThread[threadIndex] {
							DeploymentSucceededTimeMaxPerThread[threadIndex] = dur
						}
						SuccessfulDeploymentsPerThread[threadIndex] += 1
						MetricsWrapper(MetricsController, metricsConstants.CollectorDeployments, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulDeploymentsCreationCounter)
						increaseBar(deploymentsBar, deploymentsBarMutex)
					}
					return deploymentIsDone, nil
				})
				// Only timeout error returns here
				if err != nil {
					if conditionError != nil {
						// If timeout error occured but conditionError is set, it means the timeout has occurred related to GetDeployment or checkDeploymentFailed functions
						// The idea is that deployment errors can disappear until a successful deployment occurs
						dur := lastUpdateTimeOfFailed.Time.Sub(creationTimestamp.Time)
						DeploymentFailedTimeSumPerThread[threadIndex] += dur
						logError(21, fmt.Sprintf("Deployment for applicationName/componentName %s/%s failed : %v", applicationName, componentName, conditionError))
					} else {
						// regular timeout error
						logError(22, fmt.Sprintf("Deployment for applicationName/componentName %s/%s failed to succeed within %v: %v", applicationName, componentName, deploymentTimeout, err))
					}
					FailedDeploymentsPerThread[threadIndex] += 1
					MetricsWrapper(MetricsController, metricsConstants.CollectorDeployments, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedDeploymentsCreationCounter)
					increaseBar(deploymentsBar, deploymentsBarMutex)
				}
			}
		}()
	}
	wg.Wait()

	// Save the trace records to JSON
	err := createTraceDataJSON(fmt.Sprintf("%s/trace.json", outputDir), traceRecords)
	if err != nil {
		klog.Errorf("error while marshalling JSON: %v\n", err)
	}
}

func checkDeploymentFailed(deployment *appsv1.Deployment) (bool, string, metav1.Time) {
	var lastUpdateTime metav1.Time = metav1.Now() // initialize with the current time

	// Check if the Deployment is in a stable state
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false, "", lastUpdateTime
	}

	/* Iterate over all conditions
		   https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#failed-deployment
	       All the below tests catches all the described failure reasons
	*/
	for _, condition := range deployment.Status.Conditions {
		switch condition.Type {
		case appsv1.DeploymentAvailable:
			if condition.Status == corev1.ConditionFalse {
				return true, fmt.Sprintf("Deployment is not available: %s", condition.Message), condition.LastUpdateTime
			}
		case appsv1.DeploymentProgressing:
			if condition.Status == corev1.ConditionFalse {
				return true, fmt.Sprintf("Deployment failed to progress: %s", condition.Message), condition.LastUpdateTime
			}
		case appsv1.DeploymentReplicaFailure:
			if condition.Status == corev1.ConditionTrue {
				return true, fmt.Sprintf("Deployment failed during rollout: %s", condition.Message), condition.LastUpdateTime
			}
		}
	}

	return false, "", lastUpdateTime
}

func checkDeploymentIsDone(deployment *appsv1.Deployment) (bool, metav1.Time) {
	var deploymentCompleted bool = false
	var deploymentReachedDesiredState bool = false
	var lastUpdateTime metav1.Time = metav1.Now() // initialize with the current time

	// Check if the Deployment is in a stable state
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false, lastUpdateTime
	}

	/* Check the DeploymentProgressing condition
	   https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment

	   When the rollout becomes “complete”, the Deployment controller sets a condition with the following attributes to the Deployment's .status.conditions:
	   type: Progressing
	   status: "True"
	   reason: NewReplicaSetAvailable
	*/
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionTrue && condition.Reason == "NewReplicaSetAvailable" {
			// --> Deployment is complete
			deploymentCompleted = true
			lastUpdateTime = condition.LastUpdateTime
			break
		}
	}

	// Check the replica counts
	// The desired number of replicas are running, ready, and have the updated version of the app
	if *deployment.Spec.Replicas == deployment.Status.ReadyReplicas && *deployment.Spec.Replicas == deployment.Status.UpdatedReplicas {
		// --> Deployment has achieved desired state
		deploymentReachedDesiredState = true
	}

	return deploymentCompleted && deploymentReachedDesiredState, lastUpdateTime
}

// TraceRecord represents a single trace record
type TraceRecord struct {
	FunctionName string                 `json:"functionName"`
	Timestamp    time.Time              `json:"timestamp"`
	Params       map[string]interface{} `json:"params"`
}

var traceRecords []TraceRecord
var traceMutex sync.Mutex

// TraceFunction records the trace of the function with the given name and dynamic parameters
func TraceFunction(functionName string, params map[string]interface{}) {
	traceMutex.Lock()
	traceRecords = append(traceRecords, TraceRecord{
		FunctionName: functionName,
		Timestamp:    time.Now().UTC(),
		Params:       params,
	})
	traceMutex.Unlock()
}

func createTraceDataJSON(outputFile string, traceRecords []TraceRecord) error {
	jsonData, err := json.MarshalIndent(traceRecords, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %v", err)
	}

	err = os.WriteFile(outputFile, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("error writing JSON file: %v", err)
	}

	return nil
}
