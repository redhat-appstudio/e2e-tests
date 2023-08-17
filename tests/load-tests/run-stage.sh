go run loadtest.go \
    --component-repo "${COMPONENT_REPO:-https://github.com/devfile-samples/devfile-sample-code-with-quarkus}" \
    --username "$USER_PREFIX" \
    --users "${USERS_PER_THREAD:-1}" \
    --test-scenario-git-url "${TEST_SCENARIO_GIT_URL:-https://github.com/redhat-appstudio/integration-examples.git}" \
    --test-scenario-revision "${TEST_SCENARIO_REVISION:-main}" \
    --test-scenario-path-in-repo "${TEST_SCENARIO_PATH_IN_REPO:-pipelines/integration_resolver_pipeline_pass.yaml}" \
    -s \
    -w="${WAIT_PIPELINES:-true}" \
    -i="${WAIT_INTEGRATION_TESTS:-true}" \
    -d="${WAIT_DEPLOYMENTS:-true}" \
    -l \
    -o "${OUTPUT_DIR:-.}" \
    -t "${THREADS:-1}" \
    --disable-metrics \
    --pipeline-skip-initial-checks="${PIPELINE_SKIP_INITIAL_CHECKS:-true}"
