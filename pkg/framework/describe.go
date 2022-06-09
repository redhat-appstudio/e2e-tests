package framework

import (
	. "github.com/onsi/ginkgo/v2"
)

// HASSuiteDescribe annotates the application service tests with the application label.
func HASSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[has-suite "+text+"]", args, Ordered, body)
}

// E2ESuiteDescribe annotates the e2e scenarios tests with the e2e-scenarios label.
func E2ESuiteDescribe(args interface{}, body func()) bool {
	return Describe("[e2e-demos-suite]", args, Ordered, body)
}

// CommonSuiteDescribe annotates the common tests with the application label.
func CommonSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[common-suite "+text+"]", args, Ordered, body)
}

func ChainsSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[chains-suite "+text+"]", args, Ordered, body)
}

func BuildSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[build-service-suite "+text+"]", args, Ordered, body)
}

func ClusterRegistrationSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[cluster-registration-suite "+text+"]", args, Ordered, body)
}

func ReleaseSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[release-suite "+text+"]", Ordered, body)
}

func IntegrationServiceSuiteDescribe(text string, args interface{}, body func()) bool {
	return Describe("[integration-service-suite "+text+"]", Ordered, body)
}
