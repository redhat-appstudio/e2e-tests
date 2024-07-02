#!/bin/bash
export MY_GITHUB_ORG GITHUB_TOKEN

OUTPUT_DIR="${OUTPUT_DIR:-.}"
USER_PREFIX=${USER_PREFIX:-testuser}

export TEKTON_PERF_PROFILE_CPU_PERIOD=${TEKTON_PERF_PROFILE_CPU_PERIOD:-${THRESHOLD:-300}}

OPENSHIFT_API="${OPENSHIFT_API:-$(yq '.clusters[0].cluster.server' "$KUBECONFIG")}"
OPENSHIFT_USERNAME="${OPENSHIFT_USERNAME:-kubeadmin}"
OPENSHIFT_PASSWORD="${OPENSHIFT_PASSWORD:-$(cat "$KUBEADMIN_PASSWORD_FILE")}"

load_test() {
    local workdir threads index
    workdir=${1:-/tmp}
    threads=${2:-1}
    index=$(printf "%04d" "$threads")
    echo
    echo "=== RHTAP load test ==="
    echo "Threads: $threads"
    echo "Iteration: $(basename "$workdir")"
    echo
    ## Enable CPU profiling in Tekton
    if [ "${TEKTON_PERF_ENABLE_CPU_PROFILING:-}" == "true" ]; then
        echo "Starting CPU profiling of Tekton results watcher with pprof"
        for p in $(oc get pods -n tekton-results -l app.kubernetes.io/name=tekton-results-watcher -o name); do
            pod="${p##*/}"
            file="tekton-results-watcher.$pod.cpu-profile"
            oc exec -n tekton-results "$p" -c watcher -- bash -c "curl -SsL --max-time $((TEKTON_PERF_PROFILE_CPU_PERIOD + 10)) localhost:8008/debug/pprof/profile?seconds=${TEKTON_PERF_PROFILE_CPU_PERIOD} | base64" | base64 -d >"$workdir/$file.pprof" &
            echo $! >"$workdir/$file.pid"
            file=tekton-results-watcher.$pod.cpu-profile.mutex
            oc exec -n tekton-results "$p" -c watcher -- bash -c "curl -SsL --max-time $((TEKTON_PERF_PROFILE_CPU_PERIOD + 10)) localhost:8008/debug/pprof/mutex?seconds=${TEKTON_PERF_PROFILE_CPU_PERIOD} | base64" | base64 -d >"$workdir/$file.pprof" &
            echo $! >"$workdir/$file.pid"
        done
        echo "Starting CPU profiling of Tekton results API with pprof"
        for p in $(oc get pods -n tekton-results -l app.kubernetes.io/name=tekton-results-api -o name); do
            pod="${p##*/}"
            file=tekton-results-api.$pod.cpu-profile
            oc exec -n tekton-results "$p" -c api -- bash -c "curl -SsL --max-time $((TEKTON_PERF_PROFILE_CPU_PERIOD + 10)) localhost:6060/debug/pprof/profile?seconds=${TEKTON_PERF_PROFILE_CPU_PERIOD} | base64" | base64 -d >"$workdir/$file.pprof" &
            echo $! >"$workdir/$file.pid"
            file=tekton-results-api.$pod.cpu-profile.mutex
            oc exec -n tekton-results "$p" -c api -- bash -c "curl -SsL --max-time $((TEKTON_PERF_PROFILE_CPU_PERIOD + 10)) localhost:6060/debug/pprof/mutex?seconds=${TEKTON_PERF_PROFILE_CPU_PERIOD} | base64" | base64 -d >"$workdir/$file.pprof" &
            echo $! >"$workdir/$file.pid"
        done
    fi
    ## Enable memory profiling in Tekton
    if [ "${TEKTON_PERF_ENABLE_MEMORY_PROFILING:-}" == "true" ]; then
        echo "Starting memory profiling of Tekton results watcher with pprof"
        for p in $(oc get pods -n tekton-results -l app.kubernetes.io/name=tekton-results-watcher -o name); do
            pod="${p##*/}"
            file="tekton-results-watcher.$pod.memory-profile"
            oc exec -n tekton-results "$p" -c watcher -- bash -c "curl -SsL --max-time $((TEKTON_PERF_PROFILE_CPU_PERIOD + 10)) localhost:8008/debug/pprof/heap?seconds=${TEKTON_PERF_PROFILE_CPU_PERIOD} | base64" | base64 -d >"$workdir/$file.pprof" &
            echo $! >"$workdir/$file.pid"
        done
        echo "Starting memory profiling of Tekton results API with pprof"
        for p in $(oc get pods -n tekton-results -l app.kubernetes.io/name=tekton-results-api -o name); do
            pod="${p##*/}"
            file=tekton-results-api.$pod.memory-profile
            oc exec -n tekton-results "$p" -c api -- bash -c "curl -SsL --max-time $((TEKTON_PERF_PROFILE_CPU_PERIOD + 10)) localhost:8008/debug/pprof/heap?seconds=${TEKTON_PERF_PROFILE_CPU_PERIOD} | base64" | base64 -d >"$workdir/$file.pprof" &
            echo $! >"$workdir/$file.pid"
        done
    fi
    rm -rvf "$workdir/load-test.json"
    rm -rvf "$workdir/load-test.log"

    date -Ins --utc >started
    go run loadtest.go \
        --applications-count "${APPLICATIONS_COUNT:-1}" \
        --build-pipeline-selector-bundle "${BUILD_PIPELINE_SELECTOR_BUNDLE:-}" \
        --component-repo "${COMPONENT_REPO:-https://github.com/nodeshift-starters/devfile-sample}" \
        --component-repo-container-context "${COMPONENT_REPO_CONTAINER_CONTEXT:-/}" \
        --component-repo-container-file "${COMPONENT_REPO_CONTAINER_FILE:-Dockerfile}" \
        --component-repo-revision "${COMPONENT_REPO_REVISION:-main}" \
        --components-count "${COMPONENTS_COUNT:-1}" \
        --concurrency "$threads" \
        --journey-duration "${JOURNEY_DURATION:-1h}" \
        --journey-repeats "${JOURNEY_REPEATS:-1}" \
        --log-info \
        --multiarch-workflow="${MULTIARCH_WORKFLOW:-false}" \
        --output-dir "${workdir:-/tmp}" \
        --pipeline-request-configure-pac="${PIPELINE_REQUEST_CONFIGURE_PAC:-false}" \
        --pipeline-skip-initial-checks="${PIPELINE_SKIP_INITIAL_CHECKS:-true}" \
        --purge="${PURGE:-true}" \
        --quay-repo "${QUAY_REPO:-stonesoup_perfscale}" \
        --test-scenario-git-url "${TEST_SCENARIO_GIT_URL:-https://github.com/konflux-ci/integration-examples.git}" \
        --test-scenario-path-in-repo "${TEST_SCENARIO_PATH_IN_REPO:-pipelines/integration_resolver_pipeline_pass.yaml}" \
        --test-scenario-revision "${TEST_SCENARIO_REVISION:-main}" \
        --username "$USER_PREFIX-$index" \
        --waitintegrationtestspipelines="${WAIT_INTEGRATION_TESTS:-true}" \
        --waitpipelines="${WAIT_PIPELINES:-true}" \
        2>&1 | tee "$workdir/load-test.log"
    date -Ins --utc >ended

    set +u
    source venv/bin/activate
    set -u

    echo "[$(date --utc -Ins)] Create summary JSON with timings"
    ./evaluate.py "$workdir/load-test-timings.csv" "$workdir/load-test-timings.json"

    echo "[$(date --utc -Ins)] Creating main status data file"
    STATUS_DATA_FILE="$workdir/load-test.json"
    status_data.py \
        --status-data-file "${STATUS_DATA_FILE}" \
        --set "name=Konflux loadtest" "started=$(cat started)" "ended=$(cat ended)" \
        --set-subtree-json "parameters.options=$workdir/load-test-options.json" "results.measurements=$workdir/load-test-timings.json"

    deactivate

    if [ "${TEKTON_PERF_ENABLE_CPU_PROFILING:-}" == "true" ] || [ "${TEKTON_PERF_ENABLE_MEMORY_PROFILING:-}" == "true" ]; then
        echo "Waiting for the Tekton profiling to finish up to ${TEKTON_PERF_PROFILE_CPU_PERIOD}s"
        for pid_file in $(find $output_dir -name 'tekton*.pid'); do
            wait "$(cat "$pid_file")"
            rm -rvf "$pid_file"
        done
        echo "Getting Tekton results watcher goroutine dump"
        for p in $(oc get pods -n tekton-results -l app.kubernetes.io/name=tekton-results-watcher -o name); do
            pod="${p##*/}"
            for i in 0 1 2; do
                file="tekton-results-watcher.$pod.goroutine-dump-$i"
                oc exec -n tekton-results "$p" -c watcher -- bash -c "curl -SsL localhost:8008/debug/pprof/goroutine?debug=$i | base64" | base64 -d >"$workdir/$file.pprof"
            done
        done
        echo "Getting Tekton results api goroutine dump"
        for p in $(oc get pods -n tekton-results -l app.kubernetes.io/name=tekton-results-api -o name); do
            pod="${p##*/}"
            for i in 0 1 2; do
                file="tekton-results-api.$pod.goroutine-dump-$i"
                oc exec -n tekton-results "$p" -c api -- bash -c "curl -SsL localhost:6060/debug/pprof/goroutine?debug=$i | base64" | base64 -d >"$workdir/$file.pprof"
            done
        done
    fi

    echo "[$(date --utc -Ins)] Finished processing results"
}

remove_finalizers() {
    res=$1
    while [ "$(oc get "$res" -A -o json | jq -rc '.items[] | select(.metadata.namespace | startswith("'"$USER_PREFIX"'"))' | wc -l)" != "0" ]; do
        echo "## Removing finalizers for all $res"
        while read -r line; do
            echo "$line '{\"metadata\":{\"finalizers\":[]}}' --type=merge;"
        done <<<"$(oc get "$res" -A -o json | jq -rc '.items[] | select(.metadata.namespace | startswith("'"$USER_PREFIX"'")) | "oc patch '"$res"' " + .metadata.name + " -n " + .metadata.namespace + " -p "')" | bash -s
    done
}

clean_namespaces() {
    echo "Deleting resources from previous steps"
    for res in pipelineruns.tekton.dev components.appstudio.redhat.com componentdetectionqueries.appstudio.redhat.com snapshotenvironmentbindings.appstudio.redhat.com applications.appstudio.redhat.com; do
        echo -e " * $res"
        if [ -n "${DELETE_INCLUDE_FINALIZERS:-}" ]; then
            remove_finalizers "$res" &
            echo "## Deleting all $res"
        fi
        oc get "$res" -A -o json | jq -rc '.items[] | select(.metadata.namespace | startswith("'"$USER_PREFIX"'"))| "oc delete '"$res"' " + .metadata.name + " -n " + .metadata.namespace + " --ignore-not-found=true;"' | bash -s
    done
    oc get usersignups.toolchain.dev.openshift.com -A -o name | grep "$USER_PREFIX" | xargs oc delete -n toolchain-host-operator --ignore-not-found=true
    attempts=60
    attempt=1
    sleep="5s"
    while [ "$attempt" -le "$attempts" ]; do
        echo " * Waiting $sleep until all namespaces with '$USER_PREFIX' prefix are gone [attempt $attempt/$attempts]"
        oc get ns | grep -E "^$USER_PREFIX" >/dev/null 2>&1 || break 1
        sleep "$sleep"
        attempt=$((attempt + 1))
    done
    if [ "$attempt" -le "$attempts" ]; then
        echo " * All the namespaces with '$USER_PREFIX' are gone!"
    else
        echo " * WARNING: Timeout waiting for namespaces with '$USER_PREFIX' to be gone. The following namespaces still exist:"
        oc get ns | grep -E "^$USER_PREFIX"
    fi
}

max_concurrency() {
    local iteration index iteration_index
    # Max length of compliant username is 20 characters. We add "-XXXX-XXXX" suffix for the test users' name so max length of the prefix is 10.
    # See https://github.com/codeready-toolchain/toolchain-common/blob/master/pkg/usersignup/usersignup.go#L16
    if [ ${#USER_PREFIX} -gt 10 ]; then
        echo "Maximal allowed length of user prefix is 10 characters. The '$USER_PREFIX' length of ${#USER_PREFIX} exceeds the limit."
        exit 1
    else
        output="$OUTPUT_DIR/load-test.max-concurrency.json"
        IFS="," read -r -a maxConcurrencySteps <<<"$(echo "${MAX_CONCURRENCY_STEPS:-1\ 5\ 10\ 25\ 50\ 100\ 150\ 200}" | sed 's/ /,/g')"
        maxThreads=${MAX_THREADS:-10}      # Do not go above this concurrency.
        threshold_sec=${THRESHOLD:-300}    # In seconds. If KPI crosses this duration, stop.
        threshold_err=${THRESHOLD_ERR:-10} # Failure ratio. When crossed, stop.
        echo '{"started":"'"$(date +%FT%T%:z)"'", "maxThreads": '"$maxThreads"', "maxConcurrencySteps": "'"${maxConcurrencySteps[*]}"'", "threshold": '"$threshold_sec"', "thresholdErrors": '"$threshold_err"', "maxConcurrencyReached": 0, "computedConcurrency": 0, "workloadKPI": 0, "ended": "", "errorsTotal": -1}' | jq >"$output"
        iteration=0

        {
            python3 -m venv venv
            set +u
            source venv/bin/activate
            set -u
            python3 -m pip install -U pip
            python3 -m pip install -e "git+https://github.com/redhat-performance/opl.git#egg=opl-rhcloud-perf-team-core&subdirectory=core"
            python3 -m pip install tabulate
            python3 -m pip install matplotlib
            deactivate
        } &>"$OUTPUT_DIR/monitoring-setup.log"

        iteration=${ITERATION_OFFSET:-0}
        for t in "${maxConcurrencySteps[@]}"; do
            iteration="$((iteration + 1))"
            if (("$t" > "$maxThreads")); then
                break
            fi
            echo "[$(date --utc -Ins)] Starting iteration ${iteration} with concurrency ${t}"
            oc login "$OPENSHIFT_API" -u "$OPENSHIFT_USERNAME" -p "$OPENSHIFT_PASSWORD"
            clean_namespaces
            iteration_index="$(printf "%04d" "$iteration")-$(printf "%04d" "$t")"
            workdir="${OUTPUT_DIR}/iteration-${iteration_index}"
            mkdir "${workdir}"
            load_test "$workdir" "$t"
            jq ".metadata.\"max-concurrency\".iteration = \"$(printf "%04d" "$iteration")\"" "$workdir/load-test.json" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$workdir/load-test.json"
            oc logs -c watcher -n tekton-results -l app.kubernetes.io/name=tekton-results-watcher --tail=-1 --prefix=true >"$workdir/tekton-results-watcher.logs"
            oc logs -c api -n tekton-results -l app.kubernetes.io/name=tekton-results-api --tail=-1 --prefix=true >"$workdir/tekton-results-api.logs"
            workloadKPI=$(jq '.results.measurements.KPI.mean' "$workdir/load-test.json")
            workloadKPIerrors=$(jq '.results.measurements.KPI.errors' "$workdir/load-test.json")
            if [ -z "$workloadKPI" ] || [ -z "$workloadKPIerrors" ] || [ "$workloadKPI" = "null" ] || [ "$workloadKPIerrors" = "null" ]; then
                echo "[$(date --utc -Ins)] Error getting test iteration results: $workloadKPI/$workloadKPIerrors"
                exit 1
            elif awk "BEGIN { exit !($workloadKPI > $threshold_sec || $workloadKPIerrors / $t * 100 > $threshold_err)}"; then
                echo "[$(date --utc -Ins)] The average time a workload took to succeed (${workloadKPI}s) or error rate (${workloadKPIerrors}/${t}) has exceeded a threshold of ${threshold_sec}s or ${threshold_err} error rate with $t threads."
                workloadKPIOld=$(jq '.workloadKPI' "$output")
                threadsOld=$(jq '.maxConcurrencyReached' "$output")
                computedConcurrency=$(python3 -c "import sys; t = float(sys.argv[1]); a = float(sys.argv[2]); b = float(sys.argv[3]); c = float(sys.argv[4]); d = float(sys.argv[5]); print((t - b) / ((d - b) / (c - a)) + a)" "$threshold_sec" "$threadsOld" "$workloadKPIOld" "$t" "$workloadKPI")
                jq ".computedConcurrency = $computedConcurrency" "$output" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$output"
                break
            else
                echo "[$(date --utc -Ins)] The average time a workload took to succeed (${workloadKPI}s) and error rate (${workloadKPIerrors}/${t}) looks good with $t threads."
                jq ".maxConcurrencyReached = $t" "$output" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$output"
                jq ".workloadKPI = $workloadKPI" "$output" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$output"
                jq ".computedConcurrency = $t" "$output" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$output"
                jq '.ended = "'"$(date +%FT%T%:z)"'"' "$output" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$output"
                jq ".errorsTotal = $workloadKPIerrors" "$output" >"$OUTPUT_DIR/$$.json" && mv -f "$OUTPUT_DIR/$$.json" "$output"
            fi
        done
    fi
}

max_concurrency
