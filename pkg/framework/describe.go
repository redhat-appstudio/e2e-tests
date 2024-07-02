package framework

import (
	. "github.com/onsi/ginkgo/v2"
)

// CommonSuiteDescribe annotates the common tests with the application label.
func CommonSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[common-suite "+text+"]", args, Ordered)
}

func BuildSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[build-service-suite "+text+"]", args)
}

func JVMBuildSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[jvm-build-service-suite "+text+"]", args, Ordered)
}

func MultiPlatformBuildSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[multi-platform-build-service-suite "+text+"]", args, Ordered)
}

func IntegrationServiceSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[integration-service-suite "+text+"]", args, Ordered)
}

func RhtapDemoSuiteDescribe(args ...interface{}) bool {
	return Describe("[rhtap-demo-suite]", args)
}

func EnterpriseContractSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[enterprise-contract-suite "+text+"]", args, Ordered)
}

func UpgradeSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[upgrade-suite "+text+"]", args, Ordered)
}

func ReleasePipelinesSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[release-pipelines-suite "+text+"]", args, Ordered)
}

func ReleaseServiceSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[release-service-suite "+text+"]", args, Ordered)
}

func TknBundleSuiteDescribe(text string, args ...interface{}) bool {
	return Describe("[task-suite "+text+"]", args, Ordered)
}
