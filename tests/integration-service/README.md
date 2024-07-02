# Integration Service

The Integration Service manages the lifecycle, deployment and integration testing of applications and components. It handles snapshots, environments, pipelines and deployments, ensuring applications run efficiently and are deployed correctly.


## Happy Path Tests

Happy path testing describes tests that focus on the most common scenarios while assuming there are no exceptions or errors.
### Basic E2E Tests (within `integration.go`):

- Testing for successful creation of applications and components.
- Asserting the successful creation of a snapshot after a push event.
- Checking if the BuildPipelineRun is successfully triggered, contains the finalizer, and got completed.
- Asserting the signing of BuildPipelineRun.
- Validating the successful creation of a Snapshot, and removal of finalizer from BuildPipelineRun.
- Verifying that all the Integration PipelineRuns finished successfully.
- Checking that the status of integration tests were reported to the Snapshot.
- Checking that a snapshot gets successfully marked as 'passed' after all Integration pipeline runs finished.
- Validating the Global Candidate is updated
- Validating the successful creation of a Release.

### E2E tests of Status Reporting of Integration tests to CheckRuns (within `status-reporting-to-pullrequest.go`):

- Creating 2 IntegrationTestScenarios: one that's supposed to pass and other one to fail.
- Creating PaC branch and component base branches which will be used to create Pull request (PR).
- Testing for successful creation of applications and component (with the above custom branch).
- Checking if the Build pipelineRun is successfully triggered and completed.
- Asserting the creation of PaC init PR within the component repo.
- Asserting the proper status reporting of Build PipelineRun within the PR's CheckRun.
- Asserting the signing of BuildPipelineRun.
- Asserting the successful creation of a Snapshot after a push event.
- Verifying that both the Integration pipelineRuns got created and finished successfully.
- Checking that a Snapshot gets successfully marked as 'failed' after all Integration pipelinRuns finished.
- Asserting the proper status reporting of both the Integration pipelineRuns within the PR's CheckRuns.

### E2E tests of Status Reporting for Integration tests PLR in CommitStatus over GitLab.com (within `gitlab-integration-reporting.go`):

- Creating 2 IntegrationTestScenarios: one that's supposed to pass and other one to fail.
- Creating PaC branch and component base branches which will be used to create Merge Request (MR).
- Testing for successful creation of applications and component (with the above custom branch).
- Checking if the Build pipelineRun is successfully triggered and completed.
- Asserting the creation of PaC MR within the component repo.
- Asserting the proper status reporting of Build PipelineRun within the MR's CommitStatus.
- Asserting the signing of BuildPipelineRun.
- Asserting the successful creation of a Snapshot after a push event.
- Verifying that both the Integration pipelineRuns got created and finished successfully.
- Checking that a Snapshot gets successfully marked as 'failed' after all Integration pipelinRuns finished.
- Asserting the proper status reporting of both the Integration pipelineRuns within the MR's CommitStatus.


Happy path testing describes tests while integration test scenario pipelineRun reuns over environment.
### E2E Tests with Environrmnrt (within `integration-with-env.go`):

- Testing for successful creation of integration test scenario pointed to pipeline: integration-examples/pipelines
/integration_resolver_pipeline_environment_pass.yaml.
- Testing for successful creation of applications and components.
- Checking if the BuildPipelineRun is successfully triggered, contains the finalizer, and got completed.
- Asserting the signing of BuildPipelineRun.
- Validating the successful creation of a Snapshot, and removal of finalizer from BuildPipelineRun.
- Verifying that CronJob "spacerequest-cleaner" exists.
- Verifying that the Integration PipelineRuns finished successfully.
- Checking that the status of integration test was reported to the Snapshot.
- Checking that a snapshot gets successfully marked as 'passed' after Integration pipeline runs finished.
- Checking if space request is created in namespace.
- Verify that space request is deleted after successful deletion of integration test scenario pipelinerun.
## Negative Test Cases

### Failed IntegrationTestScenarios (within `integration.go`):

- Creating an IntegrationTestScenario that is expected to fail.
- Asserting that a snapshot is marked as failed.
- Creating a new IntegrationTestScenario that is expected to pass.
- Updating the Snapshot with a re-run label for the new scenario.
- Validating that a new Integration PLR is created and finished.
- Asserting that the Snapshot doesn't contain re-run label, and contains the name of re-triggered pipelinerun.
- Asserting that a snapshot is still marked as failed.
- Validating that no Release CRs is created in certain scenarios.
- Checking that the global candidate does not get updated unexpectedly.

  
## Running e2e tests

- [Prepare the cluster.](https://github.com/redhat-appstudio/e2e-tests#install-appstudio-in-e2e-mode)


- To run service-level e2e test suite: integration.go

```bash
$ ./bin/e2e-appstudio --ginkgo.focus="integration-service-suite" –ginkgo.vv
```







