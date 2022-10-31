package cmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/redhat-appstudio/e2e-tests/pkg/framework"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	// TODO: Once all controllers are working on KCP activate all the current tests.
	_ "github.com/redhat-appstudio/e2e-tests/tests/has"
	_ "github.com/redhat-appstudio/e2e-tests/tests/release"

	"flag"

	"github.com/spf13/viper"

	"k8s.io/klog/v2"
)

var webhookConfigPath string
var demoSuitesPath string

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	return nil
}, func(data []byte) {})

func init() {
	rootDir, _ := os.Getwd()
	flag.StringVar(&webhookConfigPath, "webhookConfigPath", "", "path to webhook config file")
	flag.StringVar(&demoSuitesPath, "config-suites", fmt.Sprintf(rootDir+"/tests/e2e-demos/config/default.yaml"), "path to e2e demo suites definition")
}

func TestE2E(t *testing.T) {
	klog.Info("Starting Red Hat App Studio e2e tests...")
	// Setting viper configurations in cache
	viper.Set("config-suites", demoSuitesPath)

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Red Hat App Studio E2E tests")
}

var _ = ginkgo.SynchronizedAfterSuite(func() {}, func() {
	//Send webhook only it the parameter configPath is not empty
	if len(webhookConfigPath) > 0 {
		klog.Info("Send webhook")
		framework.SendWebhook(webhookConfigPath)
	}
})
