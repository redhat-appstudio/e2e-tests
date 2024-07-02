# Overview
The purpose of *this* document is to serve as a primer for developers/qe who are looking for best practices or adding new tests to RHTAP E2E framework.

## General tips

* Make sure you've implemented any required controller functionality that is required for your tests within the following files
   * `pkg/clients/<new controller directory>` - logic to interact with kube controllers via API
   * `pkg/framework/framework.go` - import the new controller and update the `Framework` struct to be able to initialize the new controller
* Every test package should be imported to [cmd/e2e_test.go](https://github.com/konflux-ci/e2e-tests/blob/main/cmd/e2e_test.go#L15).
* Every new test should have correct [labels](docs/LabelsNaming.md).
* Every test should have meaningful description with JIRA/GitHub issue key.
* (Recommended) Use JIRA integration for linking issues and commits (just add JIRA issue key in the commit message).
* When running via mage you can filter the suites run by specifying the
  `E2E_TEST_SUITE_LABEL` environment variable. For example:
  `E2E_TEST_SUITE_LABEL=ec ./mage runE2ETests`
* `klog` level can be controlled via `KLOG_VERBOSITY` environment variable. For
  example: `KLOG_VERBOSITY=9 ./mage runE2ETests` would output http requests
  issued via Kubernetes client from sigs.k8s.io/controller-runtime
* To quickly debug a test, you can run only the desired suite. Example: `./bin/e2e-appstudio --ginkgo.focus="e2e-demos-suite"`
* Split tests in multiple scenarios. It's better to debug a small scenario than a very big one

## Debuggability

If your test fails, it should provide as detailed as possible reasons for the failure in its failure message. The failure message is the string that gets passed (directly or indirectly) to ginkgo.Fail[f].

A good failure message:
* identifies the test failure
* has enough details to provide some initial understanding of what went wrong

It is good practice to include details like the object that failed some assertion in the failure message because then a) the information is available when analyzing a failure that occurred in the CI and b) it only gets logged when some assertion fails. Always dumping objects via log messages can make the test output very large and may distract from the relevant information.

Dumping structs with `format.Object` is recommended. Starting with Kubernetes 1.26, format.Object will pretty-print Kubernetes API objects or structs as YAML and omit unset fields, which is more readable than other alternatives like `fmt.Sprintf("%+v")`.

```golang
import (
    "fmt"
    "k8s.io/api/core/v1"
    "k8s.io/kubernetes/test/utils/format"
)

var pod v1.Pod
fmt.Printf("format.Object:\n%s", format.Object(pod, 1 /* indent one level */))

format.Object:
    <v1.Pod>:
        metadata:
          creationTimestamp: null
        spec:
          containers: null
        status: {}
```
## Polling and timeouts

When waiting for something to happen, use a reasonable timeout. Without it, a test might keep running until the entire test suite gets killed by the CI. **Beware that the CI under load may take a lot longer to complete some operation compared to running the same test locally**. On the other hand, a too long timeout also has drawbacks:

* When a feature is broken so that the expected state doesn’t get reached, a test waiting for that state first needs to time out before the test fails.
* If a state is expected to be reached within a certain time frame, then a timeout that is much higher will cause test runs to be considered successful although the feature was too slow. A dedicated performance test in a well-known environment may be a better solution for testing such performance expectations.

Good code that waits for something to happen meets the following criteria:
* accepts a context for test timeouts
* full explanation when it fails: when it observes some state and then encounters errors reading the state, then dumping both the latest observed state and the latest error is useful
* early abort when condition cannot be reached anymore

### Tips for writing and debugging long-running tests

* Use `ginkgo.By` to record individual steps. Ginkgo will use that information when describing where a test timed out.
* Use `gomega.Eventually` to wait for some condition. When it times out or gets stuck, the last failed assertion will be included in the report automatically. A good way to invoke it is:
```go
    Eventually(func() error {
  	   _, err = s.GetRelease("", snapshot.Name, namespace)
  	   return err
    }, 1*time.Minute, 100*time.Millisecond).Should(Succeed(), "timed out when waiting for Release to get created")
```
* Use `gomega.Consistently` to ensure that some condition is true for a while. As with `gomega.Eventually`, make assertions about the value instead of checking the value with Go code and then asserting that the code returns true.
* Both `gomega.Consistently` and `gomega.Eventually` can be aborted early via `gomega.StopPolling`.
* Avoid polling with functions that don’t take a context (`wait.Poll`, `wait.PollImmediate`, `wait.Until`, …) and replace with their counterparts that do (`wait.PollWithContext`, `wait.PollImmediateWithContext`, `wait.UntilWithContext`, …) or even better, with `gomega.Eventually`.

## E2E directory structure

This is a basic layout for RHTAP E2E framework project. It is a set of common directories for all teams in RHTAP.

* `/cmd`: Is the main for all e2e tests. Don't put a lot of code in the application directory. If you think the code can be imported and used in other projects, then it should live in the `/pkg` directory.
* `docs`: Documentation about RHTAP e2e world.
* `/magefiles`: The code definition about installing and running the e2e tests
* `/pipelines`: Tekton pipelines utilities for QE team like IC.
* `/pkg`: All used and imported packages for e2e tests.
  * `/pkg/clients`: Definition of different clients connection providers (like Slack, GitHub, and Kubernetes Server) and all API interaction with different RHTAP controllers.
  * `/pkg/constants`: Global constants of the e2e tests.
  * `/pkg/framework`: In the framework folder are all controllers initialization, tests reports and the interaction with Report Portal.
  * `/pkg/logs`: Tests logging utilities.
  * `/pkg/sandbox`: Initialize Sandbox controller to make authenticated requests to a Kubernetes server.
  * `/pkg/utils`: Util folders with all auxiliary functions used in the different RHTAP controllers. Futhermore, it also contains some tests utils that can be found in `/pkg/utils/util.go`.
* `/scripts`: Scripts to perform operations which cannot be do it at magefiles level.
* `/tests`: Folder where all RHTAP tests are defined and documented.
