package framework

import (
	. "github.com/onsi/ginkgo/v2"
)

// HASSuiteDescribe annotates the application service tests with the application label.
func HASSuiteDescribe(text string, body func()) bool {
	return Describe("[has-suite "+text+"]", Ordered, Label("has"), body)
}

// E2ESuiteDescribe annotates the e2e scenarios tests with the e2e-scenarios label.
func E2ESuiteDescribe(body func()) bool {
	return Describe("[e2e-demos-suite]", Ordered, Label("demos"), body)
}

// CommonSuiteDescribe annotates the common tests with the application label.
func CommonSuiteDescribe(text string, body func()) bool {
	return Describe("[common-suite "+text+"]", Ordered, Label("common"), body)
}

func ChainsSuiteDescribe(text string, body func()) bool {
	return Describe("[chains-suite "+text+"]", Ordered, Label("ec"), body)
}

func BuildSuiteDescribe(text string, body func()) bool {
	return Describe("[build-service-suite "+text+"]", Label("build"), body)
}

func JVMBuildSuiteDescribe(text string, body func()) bool {
	return Describe("[jvm-build-service-suite "+text+"]", Ordered, body)
}

func ClusterRegistrationSuiteDescribe(text string, body func()) bool {
	return Describe("[cluster-registration-suite "+text+"]", Ordered, Label("cluster-registration"), body)
}

func ReleaseSuiteDescribe(text string, body func()) bool {
	return Describe("[release-suite "+text+"]", Ordered, Label("release"), body)
}

func IntegrationServiceSuiteDescribe(text string, body func()) bool {
	return Describe("[integration-service-suite "+text+"]", Ordered, Label("integration-service"), body)
}
