# Test Labels

We would like to also have tests distinguished by labels, not only by folders for specific components. We can also run the tests only with specific label(s).

## Usage

See: https://onsi.github.io/ginkgo/#spec-labels 

## Types of labels
- component
- test type
- category

### Component Labels

Component Labels | Description
--- | --- 
has | HAS related tests
build | Build related tests
cluster-registration | cluster registration related tests
e2e-demos | e2e-demos related tests

### Test Types Labels

Test Types Labels | Description
--- | --- 
slow | Slow tests (See --slow-spec-threshold (https://onsi.github.io/ginkgo/#other-settings))
e2e-demos | e2e demos tests
load | Load tests
performance | Performance related tests
smoke | Critical functionality tests
serial | Tests which can’t be run in parallel
security | Security related tests

### Test Stability Labels

Test Types Labels | Description
--- | --- 
flaky | Flaky tests (all tests are stable unless marked flaky)

### Test Categorization Labels

Test Types Labels | Description
--- | --- 
customer-feedback | Test created upon feedback from any customer channel (customer issue, telemetry data, …)
demo | Tests related to milestone demos

# Tests naming 

Tests should be named like this:
```
...
Describe("[test_id:01][crit:high][posneg:negative]should work", Labels("has", "slow"), func() {
    ...
})
```

The reason for this convention is to generate test cases from ginkgo test files with modified version of polarion-generator from KubeVirt team (https://github.com/kubevirt/qe-tools/tree/main/pkg/polarion-generator).
Our modified version is on GitLab: https://gitlab.cee.redhat.com/appstudio-qe/qe-tools 
The biggest difference in our version is that we are using custom test ID instead of Polarion generated IDs.

Modified version parses files in ginkgo format and extracts:
- test_id - generated from `Describe`, it must be unique for every test file (we need to track history)
- Title - generated from `Describe`
- Description - generated from `Describe`
- Steps - generated from `It` and `By`
- Tags - generated from `Labels`
- Additional custom fields

### Additional custom fields for a test

You can automatically generate additional test custom fields like `importance` or `negative`,
by adding it as an attribute to the test.

Custom fields

Name | Supported Values
--- | --- 
crit | critical, high, medium, low
posneg | positive, negative
level | component, integration, system, acceptance
rfe_id | requirement id (project name parameter will become the rfe id prefix)
test_id | test id (project name parameter will become the test id prefix)