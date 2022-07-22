package utils

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/onsi/gomega"
	"github.com/redhat-appstudio/application-service/pkg/devfile"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"k8s.io/klog/v2"
)

type FailedPipelineRunDetails struct {
	FailedTaskRunName   string
	PodName             string
	FailedContainerName string
}

// CheckIfEnvironmentExists return true/false if the environment variable exists
func CheckIfEnvironmentExists(env string) bool {
	_, exist := os.LookupEnv(env)
	return exist
}

// Retrieve an environment variable. If will not exists will be used a default value
func GetEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

/*
	Right now DevFile status in HAS is a string:
	metadata:
		attributes:
			appModelRepository.url: https://github.com/redhat-appstudio-qe/pet-clinic-application-service-establish-danger
			gitOpsRepository.url: https://github.com/redhat-appstudio-qe/pet-clinic-application-service-establish-danger
		name: pet-clinic
		schemaVersion: 2.1.0
	The ObtainGitUrlFromDevfile extract from the string the git url associated with a application
*/
func ObtainGitOpsRepositoryName(devfileStatus string) string {
	appDevfile, err := devfile.ParseDevfileModel(devfileStatus)
	if err != nil {
		err = fmt.Errorf("error parsing devfile: %v", err)
	}
	// Get the devfile attributes from the parsed object
	devfileAttributes := appDevfile.GetMetadata().Attributes
	gitOpsRepository := devfileAttributes.GetString("gitOpsRepository.url", &err)
	parseUrl, err := url.Parse(gitOpsRepository)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	repoParsed := strings.Split(parseUrl.Path, "/")

	return repoParsed[len(repoParsed)-1]
}

func ObtainGitOpsRepositoryUrl(devfileStatus string) string {
	appDevfile, err := devfile.ParseDevfileModel(devfileStatus)
	if err != nil {
		err = fmt.Errorf("error parsing devfile: %v", err)
	}
	// Get the devfile attributes from the parsed object
	devfileAttributes := appDevfile.GetMetadata().Attributes
	gitOpsRepository := devfileAttributes.GetString("gitOpsRepository.url", &err)

	return gitOpsRepository
}

func GetQuayIOOrganization() string {
	return GetEnv(constants.QUAY_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")
}

func GetDockerConfigJson() string {
	return GetEnv(constants.DOCKER_CONFIG_JSON, "")
}

func IsPrivateHostname(url string) bool {
	// https://www.ibm.com/docs/en/networkmanager/4.2.0?topic=translation-private-address-ranges
	privateIPAddressPrefixes := []string{"10.", "172.1", "172.2", "172.3", "192.168"}
	addr, err := net.LookupIP(url)
	if err != nil {
		klog.Infof("Unknown host: %v", err)
		return true
	}

	ip := addr[0]
	for _, ipPrefix := range privateIPAddressPrefixes {
		if strings.HasPrefix(ip.String(), ipPrefix) {
			return true
		}
	}
	return false
}

func GetOpenshiftToken() (token string, err error) {
	// Get the token for the current openshift user
	tokenBytes, err := exec.Command("oc", "whoami", "--show-token").Output()
	if err != nil {
		return "", fmt.Errorf("Error obtainig oc token %s", err.Error())
	}
	return strings.TrimSuffix(string(tokenBytes), "\n"), nil
}

func GetFailedPipelineRunDetails(pipelineRun v1beta1.PipelineRun) *FailedPipelineRunDetails {
	d := &FailedPipelineRunDetails{}
	for trName, trs := range pipelineRun.Status.PipelineRunStatusFields.TaskRuns {
		for _, c := range trs.Status.Conditions {
			if c.Reason == "Failed" {
				d.FailedTaskRunName = trName
				d.PodName = trs.Status.PodName
				for _, s := range trs.Status.TaskRunStatusFields.Steps {
					if s.Terminated.Reason == "Error" {
						d.FailedContainerName = s.ContainerName
						return d
					}
				}
			}
		}
	}
	return d
}
