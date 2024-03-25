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

	"github.com/devfile/library/v2/pkg/util"
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
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
)

type UserAppsCompsMap struct {
	mutex sync.RWMutex
	Users map[string]*UserInfo
}

type UserInfo struct {
	Framework    framework.Framework
	Applications map[string]*AppInfo
}

type AppInfo struct {
	Components               []string
	IntegrationTestScenarios []string
}

// AddUser adds a new user to the map
func (u *UserAppsCompsMap) AddUser(userName string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	u.Users[userName] = &UserInfo{}
}

// SetUserFramework sets given framework for the user
func (u *UserAppsCompsMap) SetUserFramework(userName string, framework *framework.Framework) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalf("Can not set framework for nonexistent user %s", userName)
	}

	userInfo.Framework = *framework
}

// AddApplication adds a new application to a user
func (u *UserAppsCompsMap) AddApplication(userName, appName string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	// Ensure the user exists
	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalf("Can not add application for nonexistent user %s", userName)
	}

	// Create the application entry if it doesn't exist
	if userInfo.Applications == nil {
		userInfo.Applications = make(map[string]*AppInfo)
	}

	// Add the application to the user's applications
	userInfo.Applications[appName] = &AppInfo{}
	klog.V(5).Infof("AddApplication added userName: %s, appName: %s", userName, appName)
}

// AddComponent adds a new component to a specific application for a user
func (u *UserAppsCompsMap) AddComponent(userName, appName, compName string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	// Ensure the user exist
	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalf("Can not add component for nonexistent user %s", userName)
	}

	// Ensure application exist
	appInfo, ok := userInfo.Applications[appName]
	if !ok {
		klog.Fatalf("Can not add component for nonexistent application %s", appName)
	}

	// Add the component to the application's components list
	appInfo.Components = append(appInfo.Components, compName)
	klog.V(5).Infof("AddComponent added userName: %s, appName: %s, compName: %s", userName, appName, compName)
}

// AddIntegrationTestScenario adds a new ITS to a specific application for a user
func (u *UserAppsCompsMap) AddIntegrationTestScenario(userName, appName, itsName string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	// Ensure the user exist
	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalf("Can not add integration test scenario for nonexistent user %s", userName)
	}

	// Ensure application exist
	appInfo, ok := userInfo.Applications[appName]
	if !ok {
		klog.Fatalf("Can not add integration test scenario for nonexistent application %s", appName)
	}

	// Add the integration test scenario to the application's components list
	appInfo.IntegrationTestScenarios = append(appInfo.IntegrationTestScenarios, itsName)
	klog.V(5).Infof("AddIntegrationTestScenario added userName: %s, appName: %s, itsName: %s", userName, appName, itsName)
}

// GetUsers retrieves list of users
func (u *UserAppsCompsMap) GetUsers() []string {
	u.mutex.RLock() // Use Read lock for retrieving data
	defer u.mutex.RUnlock()

	users := make([]string, 0, len(u.Users))
	for user := range u.Users {
		users = append(users, user)
	}
	return users
}

// GetUserFramework retrieves user framework
func (u *UserAppsCompsMap) GetUserFramework(userName string) *framework.Framework {
	u.mutex.RLock()
	defer u.mutex.RUnlock()

	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalln("Can not get framework for nonexistent application")
	}

	return &userInfo.Framework
}

// GetUserApps retrieves a list of applications for a specific user
func (u *UserAppsCompsMap) GetUserApps(userName string) []string {
	u.mutex.RLock()
	defer u.mutex.RUnlock()

	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalln("Can not get applications for nonexistent user")
	}

	apps := make([]string, 0, len(userInfo.Applications))
	for app := range userInfo.Applications {
		apps = append(apps, app)
	}
	return apps
}

// GetAppComps retrieves a list of components for specific user and application
func (u *UserAppsCompsMap) GetAppComps(userName, appName string) []string {
	u.mutex.RLock()
	defer u.mutex.RUnlock()

	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalln("Can not get components for nonexistent user")
	}

	appInfo, ok := userInfo.Applications[appName]
	if !ok {
		klog.Fatalln("Can not get components for nonexistent application")
	}

	return appInfo.Components
}

// GetIntegrationTestScenarios retrieves a list of components for specific user and application
func (u *UserAppsCompsMap) GetIntegrationTestScenarios(userName, appName string) []string {
	u.mutex.RLock()
	defer u.mutex.RUnlock()

	userInfo, ok := u.Users[userName]
	if !ok {
		klog.Fatalln("Can not get integration test scenarios for nonexistent user")
	}

	appInfo, ok := userInfo.Applications[appName]
	if !ok {
		klog.Fatalln("Can not get integration test scenarios for nonexistent application")
	}

	return appInfo.IntegrationTestScenarios
}

var (
	componentRepoUrl              string = "https://github.com/devfile-samples/devfile-sample-code-with-quarkus"
	componentsCount               int    = 1
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

	UserCreationTimeSumPerThread          []time.Duration
	ApplicationCreationTimeSumPerThread   []time.Duration
	ItsCreationTimeSumPerThread           []time.Duration
	CDQCreationTimeSumPerThread           []time.Duration
	ComponentCreationTimeSumPerThread     []time.Duration
	PipelineRunSucceededTimeSumPerThread  []time.Duration
	PipelineRunFailedTimeSumPerThread     []time.Duration
	PipelineRunWaitTimeForPVCSumPerThread []time.Duration

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
	ComponentsCount                   int     `json:"componentsCount"`
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

type JourneyContext struct {
	FrameworkMap                 *sync.Map
	threadsWG                    *sync.WaitGroup
	innerThreadWG                *sync.WaitGroup
	ThreadIndex                  int
	AppStudioUsersBar            *uiprogress.Bar
	ApplicationsBar              *uiprogress.Bar
	ItsBar                       *uiprogress.Bar
	CDQsBar                      *uiprogress.Bar
	ComponentsBar                *uiprogress.Bar
	PipelinesBar                 *uiprogress.Bar
	IntegrationTestsPipelinesBar *uiprogress.Bar
	DeploymentsBar               *uiprogress.Bar
	ChUsers                      chan string
	ChPipelines                  chan string
	ChIntegrationTestsPipelines  chan string
	ChDeployments                chan string
	userAppsCompsMap             UserAppsCompsMap
}
var journeyContexts []*JourneyContext   // pointers to all thread contexts

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

func init() {
	rootCmd.Flags().StringVar(&componentRepoUrl, "component-repo", componentRepoUrl, "the component repo URL to be used")
	rootCmd.Flags().IntVar(&componentsCount, "components-count", componentsCount, "number of components to create per application")
	rootCmd.Flags().StringVar(&usernamePrefix, "username", usernamePrefix, "the prefix used for usersignup names")
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
	if verbose {
		setKlogFlag(fs, "v", "5")
	}

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
		stageUsers, err = loadtestUtils.LoadStageUsers(constants.JsonStageUsersPath)
		if err != nil {
			klog.Fatalf("Error Loading Stage Users from the given Path Please check file/contents exists: %v", err)
		}

		selectedUsers, err = loadtestUtils.SelectUsers(stageUsers, numberOfUsers, threadCount, len(stageUsers))
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
		ComponentsCount:           componentsCount,
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

	errorCountMap = make(map[int]ErrorCount)

	rand.Seed(time.Now().UnixNano())
	threadsWG = &sync.WaitGroup{}
	threadsWG.Add(threadCount)

	for threadIndex := 0; threadIndex < threadCount; threadIndex++ {
		// Create a new context for each goroutine
		threadCtx := &JourneyContext{
			threadsWG:                    threadsWG,
			innerThreadWG:                new(sync.WaitGroup),
			ThreadIndex:                  threadIndex,
			AppStudioUsersBar:            AppStudioUsersBar,
			ApplicationsBar:              ApplicationsBar,
			ItsBar:                       itsBar,
			CDQsBar:                      CDQsBar,
			ComponentsBar:                ComponentsBar,
			PipelinesBar:                 PipelinesBar,
			IntegrationTestsPipelinesBar: IntegrationTestsPipelinesBar,
			DeploymentsBar:               DeploymentsBar,
			ChUsers:                      make(chan string, numberOfUsers),
			ChPipelines:                  make(chan string, numberOfUsers*componentsCount),
			ChIntegrationTestsPipelines:  make(chan string, numberOfUsers*componentsCount),
			ChDeployments:                make(chan string, numberOfUsers*componentsCount),
			userAppsCompsMap:             UserAppsCompsMap{mutex: sync.RWMutex{}, Users: make(map[string]*UserInfo)},
		}
		journeyContexts = append(journeyContexts, threadCtx)

		go userJourneyThread(threadCtx)
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
	if PVCCreationSuccessCount > 0 {
		averageWaitTimeForPVCProvisioning = sumDurationFromArray(PipelineRunWaitTimeForPVCSumPerThread).Seconds() / float64(PVCCreationSuccessCount)
	}
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
		StageCleanup(journeyContexts)
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
	if !stage {
		klog.Infof("Avg time to provision PVC : %.2f s", averageWaitTimeForPVCProvisioning)
	}

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

func StageCleanup(journeyContexts []*JourneyContext) {
	klog.V(5).Infof("StageCleanup start")
	defer klog.V(5).Infof("StageCleanup end")

	for _, journeyCtx := range journeyContexts {
		for _, username := range journeyCtx.userAppsCompsMap.GetUsers() {
			framework := journeyCtx.userAppsCompsMap.GetUserFramework(username)
			err := framework.AsKubeDeveloper.HasController.DeleteAllApplicationsInASpecificNamespace(framework.UserNamespace, 5*time.Minute)
			if err != nil {
				klog.Errorf("while deleting resources for username: %s, got error: %v\n", username, err)
			}

			err = framework.AsKubeDeveloper.HasController.DeleteAllComponentDetectionQueriesInASpecificNamespace(framework.UserNamespace, 5*time.Minute)
			if err != nil {
				klog.Errorf("while deleting component detection queries for username: %s, got error: %v\n", username, err)
			}
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

func tryNewFramework(username string, user loadtestUtils.User, timeout time.Duration) (*framework.Framework, error) {
	ch := make(chan *framework.Framework)
	var fw *framework.Framework
	var err error
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

type Handler interface {
	SetNext(handler Handler)
	Handle(ctx *JourneyContext)
}

type BaseHandler struct {
	next Handler
}

func (h *BaseHandler) SetNext(handler Handler) {
	h.next = handler
}

func (h *BaseHandler) Handle(ctx *JourneyContext) {
	if h.next != nil {
		h.next.Handle(ctx)
	}
}

type ConcreteHandlerUsers struct {
	BaseHandler
}

func (h *ConcreteHandlerUsers) Handle(ctx *JourneyContext) {
	go func() {
		klog.V(5).Infof("ConcreteHandlerUsers thread start")
		defer klog.V(5).Infof("ConcreteHandlerUsers thread end")
		defer ctx.innerThreadWG.Done()

		for userIndex := 1; userIndex <= numberOfUsers; userIndex++ {
			startTime := time.Now()

			var username string
			if randomString {
				// Create a 5 characters wide random string to be added to username (https://issues.redhat.com/browse/RHTAP-1338)
				randomStr := util.GenerateRandomString(5)
				username = fmt.Sprintf("%s-%s-%04d", usernamePrefix, randomStr, ctx.ThreadIndex*numberOfUsers+userIndex)
			} else {
				username = fmt.Sprintf("%s-%04d", usernamePrefix, ctx.ThreadIndex*numberOfUsers+userIndex)
			}

			var user loadtestUtils.User
			var framework *framework.Framework
			var err error
			if stage {
				user = selectedUsers[ctx.ThreadIndex*numberOfUsers+userIndex-1]
				username = user.Username
				framework, err = tryNewFramework(username, user, 60*time.Minute)
			} else {
				framework, err = tryNewFramework(username, user, 60*time.Minute)
			}
			if err != nil {
				logError(1, fmt.Sprintf("Unable to provision user '%s': %v", username, err))
				FailedUserCreationsPerThread[ctx.ThreadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorUsers, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedUserCreationsCounter)
				increaseBar(ctx.AppStudioUsersBar, usersBarMutex)
				continue
			} else {
				ctx.userAppsCompsMap.AddUser(username)
				ctx.userAppsCompsMap.SetUserFramework(username, framework)
				ctx.ChUsers <- username
			}

			userCreationTime := time.Since(startTime)
			UserCreationTimeSumPerThread[ctx.ThreadIndex] += userCreationTime
			MetricsWrapper(MetricsController, metricsConstants.CollectorUsers, metricsConstants.MetricTypeGuage, metricsConstants.MetricUserCreationTimeGauge, userCreationTime.Seconds())
			if userCreationTime > UserCreationTimeMaxPerThread[ctx.ThreadIndex] {
				UserCreationTimeMaxPerThread[ctx.ThreadIndex] = userCreationTime
			}

			SuccessfulUserCreationsPerThread[ctx.ThreadIndex] += 1
			MetricsWrapper(MetricsController, metricsConstants.CollectorUsers, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulUserCreationsCounter)
			increaseBar(ctx.AppStudioUsersBar, usersBarMutex)
		}
		close(ctx.ChUsers)
	}()

	// After calling ConcreteHandlerUsers's logic, trigger the next handler in a new goroutine
	if h.next != nil {
		h.next.Handle(ctx)
	}
}

type ConcreteHandlerResources struct {
	BaseHandler
}

func (h *ConcreteHandlerResources) Handle(ctx *JourneyContext) {
	go func() {
		klog.V(5).Infof("ConcreteHandlerResources thread start")
		defer klog.V(5).Infof("ConcreteHandlerResources thread end")
		defer ctx.innerThreadWG.Done()

		for username := range ctx.ChUsers {
			framework := ctx.userAppsCompsMap.GetUserFramework(username)
			if framework == nil {
				logError(2, fmt.Sprintf("Framework not found for username %s", username))
				continue
			}
			usernamespace := framework.UserNamespace

			// Handle application creation and check for success
			// Generate a random name with #combinations > 11M
			// create unique resource names that adhere to RFC 1123 Label Names
			// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
			applicationName := fmt.Sprintf("%s-app-%s", username, util.GenerateRandomString(5))
			if !h.handleApplicationCreation(ctx, framework, username, usernamespace, applicationName) {
				// If Application creation failed, continue with the next user
				continue
			}

			// Handle Integration Test Scenario Creation
			// Generate a random name with #combinations > 11M
			itsName := fmt.Sprintf("%s-its-%s", username, util.GenerateRandomString(5))
			if !h.handleIntegrationTestScenarioCreation(ctx, framework, username, usernamespace, applicationName, itsName) {
				// If its creation failed, continue with the next user
				continue
			}

			// Handle Component Detection Query Creation
			cdqName := fmt.Sprintf("%s-cdq-%s", username, util.GenerateRandomString(5))
			blnOK, cdq := h.handleCDQCreation(ctx, framework, username, usernamespace, applicationName, cdqName)
			if !blnOK {
				// If CDQ creation failed, continue with the next user
				continue
			}

			// Handle Component Creation
			if !h.handleComponentCreation(ctx, framework, username, usernamespace, applicationName, cdq) {
				// If Component creation failed, continue with the next user
				continue
			}
		}
		close(ctx.ChPipelines)
	}()

	// Trigger the next handler
	if h.next != nil {
		h.next.Handle(ctx)
	}
}

func (h *ConcreteHandlerResources) handleApplicationCreation(ctx *JourneyContext, framework *framework.Framework, username, usernamespace, applicationName string) bool {
	klog.V(5).Infof("handleApplicationCreation start username: %s, usernamespace: %s, applicationName: %s", username, usernamespace, applicationName)
	defer klog.V(5).Infof("handleApplicationCreation end username: %s, usernamespace: %s, applicationName: %s", username, usernamespace, applicationName)

	startTimeForApplication := time.Now()
	_, err := framework.AsKubeDeveloper.HasController.CreateApplicationWithTimeout(applicationName, usernamespace, 60*time.Minute)
	applicationCreationTime := time.Since(startTimeForApplication)
	if err != nil {
		logError(3, fmt.Sprintf("Unable to create the Application %s: %v", applicationName, err))
		FailedApplicationCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedApplicationCreationCounter)
		increaseBar(ctx.ApplicationsBar, applicationsBarMutex)
		return false
	}

	ApplicationCreationTimeSumPerThread[ctx.ThreadIndex] += applicationCreationTime
	MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeGuage, metricsConstants.MetricApplicationCreationTimeGauge, applicationCreationTime.Seconds())
	if applicationCreationTime > ApplicationCreationTimeMaxPerThread[ctx.ThreadIndex] {
		ApplicationCreationTimeMaxPerThread[ctx.ThreadIndex] = applicationCreationTime
	}

	return h.validateApplicationCreation(ctx, framework, applicationName, username, usernamespace, applicationCreationTime)
}

func isConditionError(condition metav1.Condition) bool {
	return condition.Status == "True" && (strings.HasPrefix(condition.Type, "Error") || strings.HasSuffix(condition.Type, "Error"))
}

// Define Structs for Grouping Parameters for "handleCondition" function to reduce number of parameters used
type ConditionDetails struct {
	Type   string
	Status metav1.ConditionStatus
}

type CreationDetails struct {
	Timestamp time.Time
	Duration  time.Duration
}

type SuccessHandler interface {
	HandleSuccess(ctx *JourneyContext, name string, timeInSeconds float64)
}

type ApplicationSuccessHandler struct {
	Username string
}

func (h ApplicationSuccessHandler) HandleSuccess(ctx *JourneyContext, appName string, timeInSeconds float64) {
	handleApplicationSuccess(ctx, h.Username, appName, timeInSeconds)
}

type ItsSuccessHandler struct {
	Username    string
	Application string
}

func (h ItsSuccessHandler) HandleSuccess(ctx *JourneyContext, itsName string, timeInSeconds float64) {
	handleItsSuccess(ctx, itsName, h.Username, h.Application, timeInSeconds)
}

type CdqSuccessHandler struct{}

func (h CdqSuccessHandler) HandleSuccess(ctx *JourneyContext, cdqName string, timeInSeconds float64) {
	handleCdqSuccess(ctx, cdqName, timeInSeconds)
}

type ComponentSuccessHandler struct {
	Username    string
	Application string
}

func (h ComponentSuccessHandler) HandleSuccess(ctx *JourneyContext, componentName string, timeInSeconds float64) {
	handleComponentSuccess(ctx, h.Username, h.Application, componentName, timeInSeconds)
}

func handleCondition(condition metav1.Condition, ctx *JourneyContext, name string, creationDetails CreationDetails, conditionDetails ConditionDetails, successHandler SuccessHandler) (bool, error) {
	klog.V(5).Infof("handleCondition start name: %s, creationDetails: %s, conditionDetails: %s, condition: %v", name, creationDetails, conditionDetails, condition)
	defer klog.V(5).Infof("handleCondition end name: %s, creationDetails: %s, conditionDetails: %s, condition: %v", name, creationDetails, conditionDetails, condition)

	if condition.Type == conditionDetails.Type && condition.Status == conditionDetails.Status {
		actualCreationTimeInSeconds := CalculateActualCreationTimeInSeconds(condition.LastTransitionTime.Time, creationDetails.Timestamp, creationDetails.Duration)
		successHandler.HandleSuccess(ctx, name, actualCreationTimeInSeconds)
		return true, nil
	}

	if isConditionError(condition) {
		return true, fmt.Errorf("%s is in Error state: %s", name, condition.Message)
	}

	return false, nil
}

func (h *ConcreteHandlerResources) validateApplicationCreation(ctx *JourneyContext, framework *framework.Framework, applicationName, username, usernamespace string, applicationCreationTime time.Duration) bool {
	applicationValidationInterval := time.Second * 20
	applicationValidationTimeout := time.Minute * 15
	var conditionError error
	var app *appstudioApi.Application

	err := utils.WaitUntilWithInterval(func() (done bool, err error) {
		app, err = framework.AsKubeDeveloper.HasController.GetApplication(applicationName, usernamespace)
		if err != nil {
			return false, fmt.Errorf("unable to get created application %s in namespace %s: %v", applicationName, usernamespace, err)
		}

		conditionError = nil
		if len(app.Status.Conditions) == 0 {
			conditionError = fmt.Errorf("application %s has 0 status conditions", applicationName)
			return false, nil
		}

		conditionDetails := ConditionDetails{
			Type:   "Created",
			Status: metav1.ConditionStatus("True"),
		}

		creationDetails := CreationDetails{
			Timestamp: app.ObjectMeta.CreationTimestamp.Time,
			Duration:  applicationCreationTime, // Assuming this is a predefined duration
		}

		for _, condition := range app.Status.Conditions {
			done, err := handleCondition(condition, ctx, applicationName, creationDetails, conditionDetails, ApplicationSuccessHandler{Username: username})
			if done || err != nil {
				return done, err
			}
		}
		return false, nil
	}, applicationValidationInterval, applicationValidationTimeout)

	if err != nil || conditionError != nil {
		handleApplicationFailure(ctx, applicationName, username, err, conditionError)
		return false
	}

	return true
}

func handleApplicationSuccess(ctx *JourneyContext, username, applicationName string, applicationActualCreationTimeInSeconds float64) {
	klog.Infof("Successfully created Application %s", applicationName)
	MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualApplicationCreationTimeGauge, applicationActualCreationTimeInSeconds)
	SuccessfulApplicationCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulApplicationCreationCounter)
	increaseBar(ctx.ApplicationsBar, applicationsBarMutex)
	ctx.userAppsCompsMap.AddApplication(username, applicationName)
}

func handleApplicationFailure(ctx *JourneyContext, applicationName string, username string, err error, conditionError error) {
	klog.Infof("Failed creating Application %s", applicationName)
	if err != nil {
		// Handle direct errors
		logError(4, fmt.Sprintf("Failed to validate Application %s for username %s due to an error: %v", applicationName, username, err))
		FailedApplicationCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedApplicationCreationCounter)
		increaseBar(ctx.ApplicationsBar, applicationsBarMutex)
	} else if conditionError != nil {
		// Handle condition errors (e.g., timeouts or other conditions)
		logError(5, fmt.Sprintf("Failed to validate Application %s for username %s due to an error: %v", applicationName, username, conditionError.Error()))
		FailedApplicationCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorApplications, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedApplicationCreationCounter)
		increaseBar(ctx.ApplicationsBar, applicationsBarMutex)
	}
}

func (h *ConcreteHandlerResources) handleIntegrationTestScenarioCreation(ctx *JourneyContext, framework *framework.Framework, username, usernamespace, applicationName, itsName string) bool {
	klog.V(5).Infof("handleIntegrationTestScenarioCreation start username: %s, usernamespace: %s, applicationName: %s, itsName: %s", username, usernamespace, applicationName, itsName)
	defer klog.V(5).Infof("handleIntegrationTestScenarioCreation end username: %s, usernamespace: %s, applicationName: %s, itsName: %s", username, usernamespace, applicationName, itsName)

	startTimeForIts := time.Now()
	_, err := framework.AsKubeDeveloper.IntegrationController.CreateIntegrationTestScenarioV2(itsName, applicationName, usernamespace, testScenarioGitURL, testScenarioRevision, testScenarioPathInRepo)
	itsCreationTime := time.Since(startTimeForIts)
	if err != nil {
		logError(6, fmt.Sprintf("Unable to create integrationTestScenario for Application %s: %v \n", applicationName, err))
		FailedItsCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationTestSenarioCreationCounter)
		increaseBar(ctx.ItsBar, itsBarMutex)
		return false
	}

	ItsCreationTimeSumPerThread[ctx.ThreadIndex] += itsCreationTime
	MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeGuage, metricsConstants.MetricIntegrationTestSenarioCreationTimeGauge, itsCreationTime.Seconds())
	if itsCreationTime > ItsCreationTimeMaxPerThread[ctx.ThreadIndex] {
		ItsCreationTimeMaxPerThread[ctx.ThreadIndex] = itsCreationTime
	}

	return h.validateIntegrationTestScenarioCreation(ctx, framework, itsName, applicationName, username, usernamespace, itsCreationTime)
}

func findTestScenarioByName(scenarios []integrationv1beta1.IntegrationTestScenario, name string) *integrationv1beta1.IntegrationTestScenario {
	for _, scenario := range scenarios {
		if scenario.Name == name {
			return &scenario
		}
	}
	return nil // Return nil if no matching scenario is found
}

func (h *ConcreteHandlerResources) validateIntegrationTestScenarioCreation(ctx *JourneyContext, framework *framework.Framework, itsName, applicationName, username, usernamespace string, itsCreationTime time.Duration) bool {
	klog.V(5).Infof("validateIntegrationTestScenarioCreation start username: %s, usernamespace: %s, applicationName: %s, itsName: %s", username, usernamespace, applicationName, itsName)
	defer klog.V(5).Infof("validateIntegrationTestScenarioCreation end username: %s, usernamespace: %s, applicationName: %s, itsName: %s", username, usernamespace, applicationName, itsName)

	integrationTestScenarioRepoInterval := time.Second * 20
	integrationTestScenarioValidationTimeout := time.Minute * 30
	var conditionError error

	err := utils.WaitUntilWithInterval(func() (done bool, err error) {
		integrationTestScenarios, err := framework.AsKubeDeveloper.IntegrationController.GetIntegrationTestScenarios(applicationName, usernamespace)
		if err != nil {
			// Return an error immediately if we cannot fetch the scenarios
			return false, fmt.Errorf("unable to get created integrationTestScenario for Application %s: %v", applicationName, err)
		}

		conditionError = nil // Reset the condition error
		testScenario := findTestScenarioByName(*integrationTestScenarios, itsName)
		if testScenario != nil {
			if len(testScenario.Status.Conditions) == 0 {
				// store the error in conditionError
				conditionError = fmt.Errorf("integrationTestScenario %s has 0 status conditions", testScenario.Name)
				return false, nil // Continue polling
			}
			creationTimestamp := testScenario.ObjectMeta.CreationTimestamp.Time

			conditionDetails := ConditionDetails{
				Type:   "IntegrationTestScenarioValid",
				Status: metav1.ConditionStatus("True"),
			}

			creationDetails := CreationDetails{
				Timestamp: creationTimestamp,
				Duration:  itsCreationTime,
			}

			for _, condition := range testScenario.Status.Conditions {
				done, err := handleCondition(condition, ctx, itsName, creationDetails, conditionDetails, ItsSuccessHandler{Username: username, Application: applicationName})
				if done || err != nil {
					return done, err
				}
			}
		}
		return false, nil // Indicate to continue polling
	}, integrationTestScenarioRepoInterval, integrationTestScenarioValidationTimeout)

	if err != nil || conditionError != nil {
		handleItsFailure(ctx, applicationName, err, conditionError)
		return false
	}
	return true
}

func handleItsSuccess(ctx *JourneyContext, itsName, username string, applicationName string, itsActualCreationTimeInSeconds float64) {
	klog.Infof("Successfully created Integration Test Scenario %s", itsName)
	MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualIntegrationTestSenarioCreationTimeGauge, itsActualCreationTimeInSeconds)
	SuccessfulItsCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulIntegrationTestSenarioCreationCounter)
	increaseBar(ctx.ItsBar, itsBarMutex)
	ctx.userAppsCompsMap.AddIntegrationTestScenario(username, applicationName, itsName)
}

func handleItsFailure(ctx *JourneyContext, applicationName string, err, conditionError error) {
	klog.Infof("Failed creating Integration Test Scenario for Application %s", applicationName)
	if err != nil {
		logError(7, fmt.Sprintf("Failed creating integrationTestScenario for Application %s due to an error: %v \n", applicationName, err))
	} else if conditionError != nil {
		logError(8, fmt.Sprintf("Failed validating integrationTestScenario for Application %s due to an error: %v", applicationName, conditionError.Error()))
	}
	FailedItsCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsSC, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationTestSenarioCreationCounter)
	increaseBar(ctx.ItsBar, itsBarMutex)
}

func (h *ConcreteHandlerResources) handleCDQCreation(ctx *JourneyContext, framework *framework.Framework, username, usernamespace, applicationName, cdqName string) (bool, *appstudioApi.ComponentDetectionQuery) {
	klog.V(5).Infof("handleCDQCreation start username: %s, usernamespace: %s, applicationName: %s", username, usernamespace, applicationName)
	defer klog.V(5).Infof("handleCDQCreation end username: %s, usernamespace: %s, applicationName: %s", username, usernamespace, applicationName)

	startTimeForCDQ := time.Now()
	cdq, err := framework.AsKubeDeveloper.HasController.CreateComponentDetectionQueryWithTimeout(cdqName, usernamespace, componentRepoUrl, "", "", "", false, 60*time.Minute)
	cdqCreationTime := time.Since(startTimeForCDQ)

	if err != nil {
		logError(9, fmt.Sprintf("Unable to create ComponentDetectionQuery %s: %v", cdqName, err))
		FailedCDQCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
		increaseBar(ctx.CDQsBar, cdqsBarMutex)
		return false, nil
	}
	if cdq.Name != cdqName {
		logError(10, fmt.Sprintf("Actual cdq name (%s) does not match expected (%s): %v", cdq.Name, cdqName, err))
		FailedCDQCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
		increaseBar(ctx.CDQsBar, cdqsBarMutex)
		return false, nil
	}
	if len(cdq.Status.ComponentDetected) == 0 {
		logError(11, fmt.Sprintf("cdq (%s) detected no component", cdq.Name))
		FailedCDQCreationsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
		increaseBar(ctx.CDQsBar, cdqsBarMutex)
		return false, nil
	}

	CDQCreationTimeSumPerThread[ctx.ThreadIndex] += cdqCreationTime
	MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeGuage, metricsConstants.MetricCDQCreationTimeGauge, cdqCreationTime.Seconds())
	if cdqCreationTime > CDQCreationTimeMaxPerThread[ctx.ThreadIndex] {
		CDQCreationTimeMaxPerThread[ctx.ThreadIndex] = cdqCreationTime
	}

	return h.validateCDQCreation(ctx, framework, cdqName, applicationName, username, usernamespace, cdqCreationTime)
}

func (h *ConcreteHandlerResources) validateCDQCreation(ctx *JourneyContext, framework *framework.Framework, cdqName, applicationName, username, usernamespace string, cdqCreationTime time.Duration) (bool, *appstudioApi.ComponentDetectionQuery) {
	cdqValidationInterval := time.Second * 20
	cdqValidationTimeout := time.Minute * 30
	var conditionError error
	var cdq *appstudioApi.ComponentDetectionQuery

	err := utils.WaitUntilWithInterval(func() (done bool, err error) {
		cdq, err = framework.AsKubeDeveloper.HasController.GetComponentDetectionQuery(cdqName, usernamespace)
		if err != nil {
			// Return an error immediately if we cannot fetch the cdq
			return false, fmt.Errorf("unable to get created cdq %s in namespace %s: %v", cdqName, usernamespace, err)
		}

		conditionError = nil // Reset the condition error
		if len(cdq.Status.Conditions) == 0 {
			// store the error in conditionError
			conditionError = fmt.Errorf("cdq %s has 0 status conditions", cdqName)
			return false, nil // Continue polling
		}
		creationTimestamp := cdq.ObjectMeta.CreationTimestamp.Time

		conditionDetails := ConditionDetails{
			Type:   "Completed",
			Status: metav1.ConditionStatus("True"),
		}

		creationDetails := CreationDetails{
			Timestamp: creationTimestamp,
			Duration:  cdqCreationTime,
		}

		for _, condition := range cdq.Status.Conditions {
			done, err := handleCondition(condition, ctx, cdqName, creationDetails, conditionDetails, CdqSuccessHandler{})
			if done || err != nil {
				return done, err
			}
		}
		return false, nil // Indicate to continue polling
	}, cdqValidationInterval, cdqValidationTimeout)

	if err != nil || conditionError != nil {
		handleCdqFailure(ctx, applicationName, err, conditionError)
		return false, nil
	}
	return true, cdq
}

func handleCdqSuccess(ctx *JourneyContext, cdqName string, cdqActualCreationTimeInSeconds float64) {
	klog.Infof("Successfully created CDQ %s", cdqName)
	MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualCDQCreationTimeGauge, cdqActualCreationTimeInSeconds)
	SuccessfulCDQCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulCDQCreationCounter)
	increaseBar(ctx.CDQsBar, cdqsBarMutex)
}

func handleCdqFailure(ctx *JourneyContext, applicationName string, err, conditionError error) {
	klog.Infof("Failed creating CDQ for Application %s", applicationName)
	if err != nil {
		logError(12, fmt.Sprintf("Failed creating CDQ for Application %s due to an error: %v \n", applicationName, err))
	} else if conditionError != nil {
		logError(13, fmt.Sprintf("Failed validating CDQ for Application %s due to an error: %v", applicationName, conditionError.Error()))
	}
	FailedCDQCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorCDQ, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedCDQCreationCounter)
	increaseBar(ctx.CDQsBar, cdqsBarMutex)
}

func (h *ConcreteHandlerResources) handleComponentCreation(ctx *JourneyContext, framework *framework.Framework, username, usernamespace, applicationName string, cdq *appstudioApi.ComponentDetectionQuery) bool {
	klog.V(5).Infof("handleComponentCreation start username: %s, usernamespace: %s, applicationName: %s", username, usernamespace, applicationName)
	defer klog.V(5).Infof("handleComponentCreation end username: %s, usernamespace: %s, applicationName: %s", username, usernamespace, applicationName)

	var (
		startTimeForComponent time.Time
		componentCreationTime time.Duration
		compStub              appstudioApi.ComponentDetectionDescription
	)

	compStubs := make([]appstudioApi.ComponentDetectionDescription, 0, len(cdq.Status.ComponentDetected))
	for _, compStub := range cdq.Status.ComponentDetected {
		compStubs = append(compStubs, compStub)
	}

	for i := 0; i < componentsCount; i++ {
		if i < len(compStubs) {
			compStub = compStubs[i]
		} else {
			compStub = compStubs[len(cdq.Status.ComponentDetected)-1]
		}
		startTimeForComponent = time.Now()
		compStub.ComponentStub.ComponentName = fmt.Sprintf("%s-comp-%d", applicationName, i)
		component, innerComponentName, err := framework.AsKubeDeveloper.HasController.CreateComponentV2(compStub.ComponentStub, usernamespace, "", "", applicationName, pipelineSkipInitialChecks, map[string]string{})
		componentCreationTime = time.Since(startTimeForComponent)

		if err != nil {
			logError(14, fmt.Sprintf("Unable to create the Component %s: %v", innerComponentName, err))
			FailedComponentCreationsPerThread[ctx.ThreadIndex] += 1
			MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedComponentCreationCounter)
			increaseBar(ctx.ComponentsBar, componentsBarMutex)
			return false
		}
		if component.Name != innerComponentName {
			logError(15, fmt.Sprintf("Actual component name (%s) does not match expected (%s): %v", component.Name, innerComponentName, err))
			FailedComponentCreationsPerThread[ctx.ThreadIndex] += 1
			MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedComponentCreationCounter)
			increaseBar(ctx.ComponentsBar, componentsBarMutex)
			return false
		}

		ComponentCreationTimeSumPerThread[ctx.ThreadIndex] += componentCreationTime
		MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeGuage, metricsConstants.MetricComponentCreationTimeGauge, componentCreationTime.Seconds())
		if componentCreationTime > ComponentCreationTimeMaxPerThread[ctx.ThreadIndex] {
			ComponentCreationTimeMaxPerThread[ctx.ThreadIndex] = componentCreationTime
		}

		validated := h.validateComponent(ctx, framework, component.Name, applicationName, username, usernamespace, componentCreationTime)
		if !validated {
			logError(30, fmt.Sprintf("Validation of component name (%s) failed with: %v", component.Name, err))
			return false
		}
	}

	ctx.ChPipelines <- username
	return true
}

func (h *ConcreteHandlerResources) validateComponent(ctx *JourneyContext, framework *framework.Framework, componentName, applicationName, username, usernamespace string, componentCreationTime time.Duration) bool {
	klog.V(5).Infof("validateComponent start username: %s, usernamespace: %s, applicationName: %s, componentName: %s", username, usernamespace, applicationName, componentName)
	defer klog.V(5).Infof("validateComponent end username: %s, usernamespace: %s, applicationName: %s, componentName: %s", username, usernamespace, applicationName, componentName)

	componentValidationInterval := time.Second * 20
	componentValidationTimeout := time.Minute * 30
	var conditionError error

	err := utils.WaitUntilWithInterval(func() (done bool, err error) {
		component, err := framework.AsKubeDeveloper.HasController.GetComponent(componentName, usernamespace)
		if err != nil {
			// Return an error immediately if we cannot fetch the component
			return false, fmt.Errorf("unable to get created component %s in namespace %s: %v", componentName, usernamespace, err)
		}

		conditionError = nil // Reset the condition error
		if len(component.Status.Conditions) == 0 {
			// store the error in conditionError
			conditionError = fmt.Errorf("component %s has 0 status conditions", componentName)
			return false, nil // Continue polling
		}
		creationTimestamp := component.ObjectMeta.CreationTimestamp.Time

		conditionDetails := ConditionDetails{
			Type:   "Created",
			Status: metav1.ConditionStatus("True"),
		}

		creationDetails := CreationDetails{
			Timestamp: creationTimestamp,
			Duration:  componentCreationTime,
		}

		for _, condition := range component.Status.Conditions {
			done, err := handleCondition(condition, ctx, componentName, creationDetails, conditionDetails, ComponentSuccessHandler{Username: username, Application: applicationName})
			if done || err != nil {
				return done, err
			}
		}
		return false, nil // Indicate to continue polling
	}, componentValidationInterval, componentValidationTimeout)

	if err != nil || conditionError != nil {
		handleComponentFailure(ctx, applicationName, err, conditionError)
		return false
	}

	return true
}

func handleComponentSuccess(ctx *JourneyContext, username, applicationName, componentName string, componentActualCreationTimeInSeconds float64) {
	klog.Infof("Successfully created Component %s", componentName)
	MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualComponentCreationTimeGauge, componentActualCreationTimeInSeconds)
	SuccessfulComponentCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricSuccessfulComponentCreationCounter)
	increaseBar(ctx.ComponentsBar, componentsBarMutex)
	ctx.userAppsCompsMap.AddComponent(username, applicationName, componentName)
}

func handleComponentFailure(ctx *JourneyContext, applicationName string, err, conditionError error) {
	klog.Infof("Failed creating Component for Application %s", applicationName)
	if err != nil {
		logError(16, fmt.Sprintf("Failed to validate component for Application %s due to an error: %v \n", applicationName, err))
	} else if conditionError != nil {
		logError(17, fmt.Sprintf("Failed to validate component for Application %s due to an error: %v \n", applicationName, conditionError.Error()))
	}

	FailedComponentCreationsPerThread[ctx.ThreadIndex] += 1
	MetricsWrapper(MetricsController, metricsConstants.CollectorComponents, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedComponentCreationCounter)
	increaseBar(ctx.ComponentsBar, componentsBarMutex)
}

type ConcreteHandlerPipelines struct {
	BaseHandler
}

func (h *ConcreteHandlerPipelines) Handle(ctx *JourneyContext) {
	if waitPipelines {
		go func() {
			klog.V(5).Infof("ConcreteHandlerPipelines thread start")
			defer klog.V(5).Infof("ConcreteHandlerPipelines thread end")
			defer ctx.innerThreadWG.Done()

			chIntegrationTestsPipelines := ctx.ChIntegrationTestsPipelines

			for username := range ctx.ChPipelines {

				framework := ctx.userAppsCompsMap.GetUserFramework(username)
				if framework == nil {
					logError(18, fmt.Sprintf("Framework not found for username %s", username))
					increaseBar(ctx.PipelinesBar, pipelinesBarMutex)
					continue
				}

				usernamespace := framework.UserNamespace

				for _, applicationName := range ctx.userAppsCompsMap.GetUserApps(username) {
					for _, componentName := range ctx.userAppsCompsMap.GetAppComps(username, applicationName) {
						h.validatePipeline(ctx, framework, componentName, applicationName, username, usernamespace)
					}
				}

			}

			close(chIntegrationTestsPipelines)
		}()
	} else {
		klog.V(5).Infof("ConcreteHandlerPipelines skipped")
	}

	// After calling ConcreteHandlerPipelines's logic, trigger the next handler in a new goroutine
	if h.next != nil {
		h.next.Handle(ctx)
	}
}

func (h *ConcreteHandlerPipelines) validatePipeline(ctx *JourneyContext, framework *framework.Framework, componentName, applicationName, username, usernamespace string) {
	klog.V(5).Infof("validatePipeline start username: %s, usernamespace: %s, applicationName: %s, componentName: %s", username, usernamespace, applicationName, componentName)
	defer klog.V(5).Infof("validatePipeline end username: %s, usernamespace: %s, applicationName: %s, componentName: %s", username, usernamespace, applicationName, componentName)

	pipelineCreatedRetryInterval := time.Second * 20
	pipelineCreatedTimeout := time.Minute * 30

	threadIndex := ctx.ThreadIndex
	chIntegrationTestsPipelines := ctx.ChIntegrationTestsPipelines
	pipelinesBar := ctx.PipelinesBar

	err, _ := h.validatePipelineCreation(ctx, framework, componentName, applicationName, usernamespace, pipelineCreatedRetryInterval, pipelineCreatedTimeout)
	if err != nil {
		logError(20, fmt.Sprintf("PipelineRun for applicationName/componentName %s/%s has not been created within %v: %v", applicationName, componentName, pipelineCreatedTimeout, err))
		FailedPipelineRunsPerThread[ctx.ThreadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedPipelineRunsCreationCounter)
		increaseBar(ctx.PipelinesBar, pipelinesBarMutex)
		return
	}

	pipelineRunRetryInterval := time.Second * 20
	pipelineRunTimeout := time.Minute * 60

	err = k8swait.PollUntilContextTimeout(context.Background(), pipelineRunRetryInterval, pipelineRunTimeout, false, func(ctx context.Context) (done bool, err error) {
		pipelineRun, err := framework.AsKubeDeveloper.HasController.GetComponentPipelineRunWithType(componentName, applicationName, usernamespace, "build", "")
		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
			return false, nil
		}
		if pipelineRun.IsDone() {
			// This PVC test could not work on stage because our test users are not admins and because as developers they don't have access to query for PVs
			if !stage {
				h.handlePVCS(threadIndex, framework, pipelineRun)
			}
			succeededCondition := pipelineRun.Status.GetCondition(apis.ConditionSucceeded)
			if succeededCondition.IsFalse() {
				dur := pipelineRun.Status.CompletionTime.Sub(pipelineRun.CreationTimestamp.Time)
				PipelineRunFailedTimeSumPerThread[threadIndex] += dur
				logError(21, fmt.Sprintf("Pipeline run for applicationName/componentName %s/%s failed due to %v: %v", applicationName, componentName, succeededCondition.Reason, succeededCondition.Message))
				FailedPipelineRunsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedPipelineRunsCreationCounter)
			} else {
				dur := pipelineRun.Status.CompletionTime.Sub(pipelineRun.CreationTimestamp.Time)
				PipelineRunSucceededTimeSumPerThread[threadIndex] += dur
				MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeGuage, metricsConstants.MetricPipelineRunsTimeGauge, dur.Seconds())
				MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualPipelineRunsTimeGauge, dur.Seconds())
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
		logError(22, fmt.Sprintf("Pipeline run for applicationName/componentName %s/%s failed to succeed within %v: %v", applicationName, componentName, pipelineRunTimeout, err))
		FailedPipelineRunsPerThread[threadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorPipelines, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedPipelineRunsCreationCounter)
		increaseBar(pipelinesBar, pipelinesBarMutex)
	}
}

func (h *ConcreteHandlerPipelines) validatePipelineCreation(ctx *JourneyContext, framework *framework.Framework, componentName, applicationName, usernamespace string, pipelineCreatedRetryInterval, pipelineCreatedTimeout time.Duration) (error, string) {
	klog.V(5).Infof("validatePipelineCreation start usernamespace: %s, applicationName: %s, componentName: %s", usernamespace, applicationName, componentName)
	defer klog.V(5).Infof("validatePipelineCreation end usernamespace: %s, applicationName: %s, componentName: %s", usernamespace, applicationName, componentName)

	var pipelineRun *pipeline.PipelineRun

	err := k8swait.PollUntilContextTimeout(context.Background(), pipelineCreatedRetryInterval, pipelineCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {
		// Searching for "build" type of pipelineRun
		pipelineRun, err = framework.AsKubeDeveloper.HasController.GetComponentPipelineRunWithType(componentName, applicationName, usernamespace, "build", "")

		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return err, ""
	} else {
		return nil, pipelineRun.Name
	}
}

func (h *ConcreteHandlerPipelines) handlePVCS(threadIndex int, framework *framework.Framework, pipelineRun *pipeline.PipelineRun) {
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
}

type ConcreteHandlerItsPipelines struct {
	ConcreteHandlerPipelines // Embedding ConcreteHandlerPipelines
}

func (h *ConcreteHandlerItsPipelines) Handle(ctx *JourneyContext) {
	if waitIntegrationTestsPipelines {
		go func() {
			klog.V(5).Infof("ConcreteHandlerItsPipelines thread start")
			defer klog.V(5).Infof("ConcreteHandlerItsPipelines thread end")
			defer ctx.innerThreadWG.Done()
			chDeployments := ctx.ChDeployments

			for username := range ctx.ChIntegrationTestsPipelines {
				for _, applicationName := range ctx.userAppsCompsMap.GetUserApps(username) {
					for _, componentName := range ctx.userAppsCompsMap.GetAppComps(username, applicationName) {
						for _, itsName := range ctx.userAppsCompsMap.GetIntegrationTestScenarios(username, applicationName) {
							h.validateItsPipeline(ctx, applicationName, componentName, itsName, username)
						}
					}
				}
			}
			close(chDeployments)
		}()
	} else {
		klog.V(5).Infof("ConcreteHandlerItsPipelines skipped")
	}

	// After calling ConcreteHandlerItsPipelines's logic, trigger the next handler in a new goroutine
	if h.next != nil {
		h.next.Handle(ctx)
	}
}

func (h *ConcreteHandlerItsPipelines) validateItsPipeline(ctx *JourneyContext, applicationName, componentName, itsName, username string) {
	klog.V(5).Infof("validateItsPipeline start username: %s, applicationName: %s, componentName: %s, itsName: %s", username, applicationName, componentName, itsName)
	defer klog.V(5).Infof("validateItsPipeline end username: %s, applicationName: %s, componentName: %s, itsName: %s", username, applicationName, componentName, itsName)

	framework := ctx.userAppsCompsMap.GetUserFramework(username)
	usernamespace := framework.UserNamespace
	threadIndex := ctx.ThreadIndex
	chDeployments := ctx.ChDeployments
	integrationTestsPipelinesBar := ctx.IntegrationTestsPipelinesBar

	snapshotCreatedRetryInterval := time.Second * 20
	snapshotCreatedTimeout := time.Minute * 30

	err, snapshotName := h.validateSnapshotCreation(ctx, framework, componentName, usernamespace, snapshotCreatedRetryInterval, snapshotCreatedTimeout)
	if err != nil {
		logError(23, fmt.Sprintf("Snapshot for applicationName/componentName %s/%s has not been created within %v: %v", applicationName, componentName, snapshotCreatedTimeout, err))
		FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
		increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
		return
	}

	IntegrationTestsPipelineCreatedRetryInterval := time.Second * 20
	IntegrationTestsPipelineCreatedTimeout := time.Minute * 30

	err = h.validateItsPipelineCreation(ctx, framework, itsName, snapshotName, usernamespace, IntegrationTestsPipelineCreatedRetryInterval, IntegrationTestsPipelineCreatedTimeout)
	if err != nil {
		logError(24, fmt.Sprintf("IntegrationTestPipelineRun for applicationName/itsName/snapshotName %s/%s/%s has not been created within %v: %v", applicationName, itsName, snapshotName, IntegrationTestsPipelineCreatedTimeout, err))
		FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
		increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
		return
	}

	IntegrationTestsPipelineRunRetryInterval := time.Second * 20
	IntegrationTestsPipelineRunTimeout := time.Minute * 60
	//var IntegrationTestsPipelineRun *pipeline.PipelineRun

	err = k8swait.PollUntilContextTimeout(context.Background(), IntegrationTestsPipelineRunRetryInterval, IntegrationTestsPipelineRunTimeout, false, func(ctx context.Context) (done bool, err error) {
		IntegrationTestsPipelineRun, err := framework.AsKubeDeveloper.IntegrationController.GetIntegrationPipelineRun(itsName, snapshotName, usernamespace)
		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
			return false, nil
		}
		if IntegrationTestsPipelineRun.IsDone() {
			if !stage {
				h.handlePVCS(threadIndex, framework, IntegrationTestsPipelineRun)
			}
			succeededCondition := IntegrationTestsPipelineRun.Status.GetCondition(apis.ConditionSucceeded)
			if succeededCondition.IsFalse() {
				dur := IntegrationTestsPipelineRun.Status.CompletionTime.Sub(IntegrationTestsPipelineRun.CreationTimestamp.Time)
				IntegrationTestsPipelineRunFailedTimeSumPerThread[threadIndex] += dur
				logError(25, fmt.Sprintf("IntegrationTestPipelineRun for applicationName/itsName/snapshotName %s/%s/%s failed due to %v: %v", applicationName, itsName, snapshotName, succeededCondition.Reason, succeededCondition.Message))
				FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
				MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
			} else {
				dur := IntegrationTestsPipelineRun.Status.CompletionTime.Sub(IntegrationTestsPipelineRun.CreationTimestamp.Time)
				IntegrationTestsPipelineRunSucceededTimeSumPerThread[threadIndex] += dur
				MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeGuage, metricsConstants.MetricIntegrationPipelineRunsTimeGauge, dur.Seconds())
				MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualIntegrationPipelineRunsTimeGauge, dur.Seconds())
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
		logError(26, fmt.Sprintf("IntegrationTestPipelineRun for applicationName/itsName/snapshotName %s/%s/%s failed to succeed within %v: %v", applicationName, itsName, snapshotName, IntegrationTestsPipelineRunTimeout, err))
		FailedIntegrationTestsPipelineRunsPerThread[threadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorIntegrationTestsPipeline, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedIntegrationPipelineRunsCreationCounter)
		increaseBar(integrationTestsPipelinesBar, integrationTestsPipelinesBarMutex)
	}

}

func (h *ConcreteHandlerItsPipelines) validateSnapshotCreation(ctx *JourneyContext, framework *framework.Framework, componentName, usernamespace string, snapshotCreatedRetryInterval, snapshotCreatedTimeout time.Duration) (error, string) {
	var snapshot *appstudioApi.Snapshot

	err := k8swait.PollUntilContextTimeout(context.Background(), snapshotCreatedRetryInterval, snapshotCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {
		snapshot, err = framework.AsKubeDeveloper.IntegrationController.GetSnapshot("", "", componentName, usernamespace)
		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return err, ""
	} else {
		return nil, snapshot.Name
	}
}

func (h *ConcreteHandlerItsPipelines) validateItsPipelineCreation(ctx *JourneyContext, framework *framework.Framework, itsName, snapshotRunName, usernamespace string, pipelineCreatedRetryInterval, pipelineCreatedTimeout time.Duration) error {
	err := k8swait.PollUntilContextTimeout(context.Background(), pipelineCreatedRetryInterval, pipelineCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {
		_, err = framework.AsKubeDeveloper.IntegrationController.GetIntegrationPipelineRun(itsName, snapshotRunName, usernamespace)
		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
			return false, nil
		}
		return true, nil
	})

	return err
}

type ConcreteHandlerDeployments struct {
	BaseHandler
}

func (h *ConcreteHandlerDeployments) Handle(ctx *JourneyContext) {
	if waitDeployments {
		go func() {
			klog.V(5).Infof("ConcreteHandlerDeployments thread start")
			defer klog.V(5).Infof("ConcreteHandlerDeployments thread end")
			defer ctx.innerThreadWG.Done()

			for username := range ctx.ChDeployments {
				for _, applicationName := range ctx.userAppsCompsMap.GetUserApps(username) {
					for _, componentName := range ctx.userAppsCompsMap.GetAppComps(username, applicationName) {
						// since username added to chDeployments only after valid framework, usernamespace, componentName, and applicationName have been created
						//  we don't need to verify validity for neither
						framework := ctx.userAppsCompsMap.GetUserFramework(username)
						h.validateDeployment(ctx, framework, applicationName, componentName, username)
					}
				}
			}
		}()
	} else {
		klog.V(5).Infof("ConcreteHandlerDeployments skipped")
	}

	// After calling ConcreteHandlerDeployments's logic, trigger the next handler in a new goroutine
	if h.next != nil {
		h.next.Handle(ctx)
	}
}

func (h *ConcreteHandlerDeployments) validateDeployment(ctx *JourneyContext, framework *framework.Framework, applicationName, componentName, username string) {
	usernamespace := framework.UserNamespace
	var deployment *appsv1.Deployment

	threadIndex := ctx.ThreadIndex
	deploymentsBar := ctx.DeploymentsBar

	// Deploy the component using gitops and check for the health
	deploymentCreatedRetryInterval := time.Second * 20
	deploymentCreatedTimeout := time.Minute * 30

	err := h.validateDeploymentCreation(ctx, framework, componentName, usernamespace, deploymentCreatedRetryInterval, deploymentCreatedTimeout)
	if err != nil {
		logError(27, fmt.Sprintf("Deployment for applicationName/componentName %s/%s has not been created within %v: %v", applicationName, componentName, deploymentCreatedTimeout, err))
		FailedDeploymentsPerThread[threadIndex] += 1
		increaseBar(deploymentsBar, deploymentsBarMutex)
		return
	}

	deploymentRetryInterval := time.Second * 20
	deploymentTimeout := time.Minute * 30
	var conditionError error
	var (
		deploymentFailed       bool
		errorMessage           string
		lastUpdateTimeOfFailed metav1.Time
		creationTimestamp      metav1.Time
	)

	err = k8swait.PollUntilContextTimeout(context.Background(), deploymentRetryInterval, deploymentTimeout, false, func(ctx context.Context) (done bool, err error) {
		conditionError = nil // Reset the condition error
		deployment, err = framework.AsKubeDeveloper.CommonController.GetDeployment(componentName, usernamespace)
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
			MetricsWrapper(MetricsController, metricsConstants.CollectorDeployments, metricsConstants.MetricTypeGuage, metricsConstants.MetricActualDeploymentsCreationTimeGauge, dur.Seconds())
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
			logError(28, fmt.Sprintf("Deployment for applicationName/componentName %s/%s failed : %v", applicationName, componentName, conditionError))
		} else {
			// regular timeout error
			logError(29, fmt.Sprintf("Deployment for applicationName/componentName %s/%s failed to succeed within %v: %v", applicationName, componentName, deploymentTimeout, err))
		}
		FailedDeploymentsPerThread[threadIndex] += 1
		MetricsWrapper(MetricsController, metricsConstants.CollectorDeployments, metricsConstants.MetricTypeCounter, metricsConstants.MetricFailedDeploymentsCreationCounter)
		increaseBar(deploymentsBar, deploymentsBarMutex)
	}

}

func (h *ConcreteHandlerDeployments) validateDeploymentCreation(ctx *JourneyContext, framework *framework.Framework, componentName, usernamespace string, deploymentCreatedRetryInterval, deploymentCreatedTimeout time.Duration) error {
	err := k8swait.PollUntilContextTimeout(context.Background(), deploymentCreatedRetryInterval, deploymentCreatedTimeout, false, func(ctx context.Context) (done bool, err error) {
		_, err = framework.AsKubeDeveloper.CommonController.GetDeployment(componentName, usernamespace)
		if err != nil {
			klog.Infof("Unable getting deployment - %v", err)
			time.Sleep(time.Millisecond * time.Duration(rand.IntnRange(10, 200)))
			return false, nil
		}
		return true, nil
	})

	return err
}

func userJourneyThread(threadCtx *JourneyContext) {
	defer threadCtx.threadsWG.Done()

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
	threadCtx.innerThreadWG.Add(addValue)

	// Using Chain of responsibility pattern to separate the userJourneyThread function separate handlers (https://refactoring.guru/design-patterns/go)
	// Initialize handlers
	HandlerUsers := &ConcreteHandlerUsers{}
	HandlerResources := &ConcreteHandlerResources{}
	HandlerPipelines := &ConcreteHandlerPipelines{}
	HandlerItsPipelines := &ConcreteHandlerItsPipelines{}
	HandlerDeployments := &ConcreteHandlerDeployments{}

	HandlerUsers.SetNext(HandlerResources)
	HandlerResources.SetNext(HandlerPipelines)
	HandlerPipelines.SetNext(HandlerItsPipelines)
	HandlerItsPipelines.SetNext(HandlerDeployments)
	HandlerDeployments.SetNext(nil)
	// Start the chain with the users handler
	HandlerUsers.Handle(threadCtx)

	threadCtx.innerThreadWG.Wait()
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

// CalculateActualCreationTimeInSeconds calculates the adjusted creation time in seconds.
// Usage: applicationActualCreationTimeInSeconds := CalculateActualCreationTimeInSeconds(lastTransitionTime, creationTimestamp, applicationCreationTime)
func CalculateActualCreationTimeInSeconds(lastTransitionTime, creationTimestamp time.Time, creationTime time.Duration) float64 {
	deltaTime := lastTransitionTime.Sub(creationTimestamp).Seconds()
	// If deltaTime is negative, discard its value in the calculation
	if deltaTime < 0 {
		return creationTime.Seconds()
	}
	return creationTime.Seconds() + deltaTime
}
