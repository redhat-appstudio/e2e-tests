package cmd

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	_ "github.com/redhat-appstudio/e2e-tests/tests/build"
	_ "github.com/redhat-appstudio/e2e-tests/tests/byoc"
	_ "github.com/redhat-appstudio/e2e-tests/tests/enterprise-contract"
	_ "github.com/redhat-appstudio/e2e-tests/tests/integration-service"
	_ "github.com/redhat-appstudio/e2e-tests/tests/release/pipelines"
	_ "github.com/redhat-appstudio/e2e-tests/tests/release/service"
	_ "github.com/redhat-appstudio/e2e-tests/tests/remote-secret"
	_ "github.com/redhat-appstudio/e2e-tests/tests/rhtap-demo"
	_ "github.com/redhat-appstudio/e2e-tests/tests/spi"
	_ "github.com/redhat-appstudio/e2e-tests/tests/upgrade"

	"flag"

	"k8s.io/klog/v2"
)

func init() {

	klog.SetLogger(ginkgo.GinkgoLogr)

	verbosity := 1
	if v, err := strconv.ParseUint(os.Getenv("KLOG_VERBOSITY"), 10, 8); err == nil {
		verbosity = int(v)
	}

	flags := &flag.FlagSet{}
	klog.InitFlags(flags)
	if err := flags.Set("v", fmt.Sprintf("%d", verbosity)); err != nil {
		panic(err)
	}
}

func TestE2E(t *testing.T) {
	klog.Info("Starting Red Hat App Studio e2e tests...")
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Red Hat App Studio E2E tests")
}
