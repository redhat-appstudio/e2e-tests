# E2E Red Hat App Studio Framework

Testing framework solution written in golang using ginkgo framework to cover Red Hat AppStudio.

# Specifications

* Instrumented tests with ginkgo v2 framework. Find more info [here](https://docs.google.com/document/d/1h28ZknXRsTLPNNiOjdHIO-F2toCzq4xoZDXbfYaBdoQ/edit#heading=h.ptojc6n4azyr).
* Use client-go to connect to Openshift Cluster.
* Ability to run the E2E tests everywhere; locally, Openshift Cluster, OSD...
* Writes out a junit XML/JSON file with tests results to a custom directory by using `--ginkgo.junit(or json)-report` flag.
* Ability to run test suites separately.

# Setup

Before executing the e2e suites you need to have deployed App Studio component/s to your cluster. Find more info about how to deploy Red Hat App Studio
in the [infra-deployments](https://github.com/redhat-appstudio/infra-deployments) repository.

Log into your openshift cluster, using `oc login -u <user> -p <password> <oc_api_url>.`

A properly setup Go workspace using **Go 1.17 is required**.

Install dependencies:

``` bash
# Install dependencies
$ go mod tidy
# Copy the dependencies to vendor folder
$ go mod vendor
# Create `e2e-appstudio` binary in bin folder. Please add the binary to the path or just execute `./bin/e2e-appstudio`
$ make build
```

## Install App Studio in e2e mode

To install Red Hat App Studio in e2e mode you can found the instructions in [docs folder](https://github.com/redhat-appstudio/e2e-tests/tree/main/docs)

## The `e2e-appstudio` command

The `e2e-appstudio` command is the root command that executes all test functionality. To obtain all available flags for the binary please use `--help` flags. All ginkgo flags and go tests are available in `e2e-appstudio` binary.
The instructions about every test suite can be found in the [tests folder](https://github.com/redhat-appstudio/e2e-tests/tree/main/tests). Find more information about how to install the e2e binary in openshift-ci in [docs folder](https://github.com/redhat-appstudio/e2e-tests/tree/main/docs)

# Develop new tests

* Create test folder under tests folder: `tests/[<application-name>]...`, e.g.
  * `tests/application-service` - all tests used owned by App Studio application team
* Every test package should be imported to `cmd/e2e_test.go`, e.g.
* Every new test should have unique test id and correct [labels](https://github.com/redhat-appstudio/e2e-tests/tree/main/docs/LabelsNaming.md)

```golang
// cmd/e2e_test.go
package common

import (
	// ensure these packages are scanned by ginkgo for e2e tests
	_ "github.com/redhat-appstudio/e2e-tests/tests/common"
	_ "github.com/redhat-appstudio/e2e-tests/tests/has"
)
```

# Debugging tests
## In vscode
There is launch configuration in `.vscode/launch.json` called `Launch demo suites`. 
Running this configuration, you'll be asked for github token and then e2e-demos suite will run with default configuration.
If you want to run/debug different suite, change `-ginkgo.focus` parameter in `.vscode/launch.json`.
# Load tests 
[Refer this](https://github.com/redhat-appstudio/e2e-tests/blob/main/docs/LoadTests.md)
