# RHTAP demo test

### Prerequisites for running the advanced build scenario against your own cluster
1. Fork https://github.com/redhat-appstudio-qe/hacbs-test-project and https://github.com/redhat-appstudio-qe/strategy-configs to your GitHub org (specified in `MY_GITHUB_ORG` env var)
2. Make sure the cluster you are about to run this test against is public (i.e. hosted on a public cloud provider)

### Description
This test simulates typical user scenario.

#### Note

In order to run this test against **staging** cluster, export following environment variables (after updating their values with valid data) and run the test suite:
```bash
export STAGEUSER_TOKEN=<Offline token used for getting Keycloak token in order to authenticate against stage> \
STAGE_SSOURL=<Keycloak URL used for authentication against stage cluster> \
STAGE_APIURL=<Toolchain API URL used for authentication against stage cluster> \
STAGE_USERNAME=<Your stage cluster username>
# Run the test suite
ginkgo --label-filter='verify-stage' ./cmd/
```


#### Simple build
1. Setup
   1. Create a user namespace

2. Test Scenario
   1. The application was created successfully and is healthy
   2. The POC environment was created successfully
   3. The Component Detection Query was created successfully
   4. The Component (simple) Build finished successfully
   5. Snapshot was created and integration test finished successfully

### Advanced build (enabled only if AdvancedBuildSpec is set)
1. Setup
   1. Create a managed namespace used by release service for validating and releasing the built image
   2. Create required resources in managed-namespace
      1. Secret with container image registry credentials (used for pushing the image to container registry)
      2. Service account that mounts the secret, including required roles and rolebindings
      3. PVC for the release pipeline
      4. Secret with cosign-public-key (used for validating the built image by EC)
      5. Release plan for the targeted application and (user) namespace
      6. Release strategy, Release plan admission
      7. Enterprise contract policy
   3. Create a new branch in a java project on GitHub (that is used as a git source of a component) that will be used as a base branch for PaC pull request (so we don't pollute default branch - `main`)
   4. Create a new application, test environment
   5. Create a new component that will trigger the "advanced" build pipelinerun
   6. Create JBSConfig and related secret in user's (dev) namespace that will trigger jvm-build-service to deploy a jvm-cache to the dev namespace, which will be used for caching java dependencies during build

2. Test scenario
   1. Verify that the initial PaC pull request was created in the component's repo (this will also trigger an advanced build pipelinerun)
   2. After merging the PR, there should be another build pipelinerun triggered in user namespace
   3. Make sure the pipelinerun completes successfully
   4. Make sure that the resulting SBOM file can be pulled from the container registry (where also the image was pushed) and it is saved in expected format
   5. Make sure that integration test pipelinerun is created and completes successfully
   6. The release pipeline should succeed and the release should be marked as successful
   7. Make sure JVM build service is used for rebuilding java component's dependencies and that all related dependency builds complete successfully

Steps to run 'rhtap-demos':

1) Follow the instructions from the [Readme](../../docs/Installation.md) scripts to install AppStudio in e2e mode
2) Run the e2e suite: `./bin/e2e-appstudio --ginkgo.label-filter="rhtap-demo"`

## Test Generator

The test specs in rhtap-demo-suite are generated dynamically using ginkgo specs.

If you want to test your own Component (repository), all you need to do is to update the `TestScenarios` variable in [scenarios.go](./config/scenarios.go)

## Run tests with private component

Red Hat AppStudio E2E framework now supports creating components from private quay.io images and GitHub repositories.

#### Environments

| Variable | Required | Explanation | Default Value |
|---|---|---|---|
| `QUAY_OAUTH_USER` | yes | A quay.io username used to push/build containers  | ''  |
| `QUAY_OAUTH_TOKEN` | yes | A quay.io token used to push/build containers. Note: the token and username must be a robot account with access to your repository | '' |

#### Setup configuration for private repositories

1. Define in your configuration for the application and the component
Example of a test scenario for GitHub private repository:

```go
var TestScenarios = []TestSpec{
    {
        Name:            "nodejs private component test",
        ApplicationName: "nodejs-private-app",
        Components: []ComponentSpec{
            {
                Name:              "nodejs-private-comp",
                Private:           true,
                Language:          "JavaScript",
                GitSourceUrl:      "https://github.com/redhat-appstudio-qe-bot/nodejs-health-check.git",
                HealthEndpoint:    "/live",
            },
        },
    },
}
```

2. Run the e2e tests
