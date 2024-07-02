#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

# shellcheck disable=SC1090
source "/usr/local/ci-secrets/redhat-appstudio-load-test/load-test-scenario.${1:-concurrent}"

pushd "${2:-.}"

output_dir="${OUTPUT_DIR:-./tests/load-tests}"

csv_delim=";"
csv_delim_quoted="\"$csv_delim\""
dt_format='"%Y-%m-%dT%H:%M:%SZ"'

artifact_logs="${ARTIFACT_DIR}/logs"
artifact_pprof="${ARTIFACT_DIR}/pprof"
artifacts_ggm="${ARTIFACT_DIR}/ggm"

collect_artifacts() {
    echo "[$(date --utc -Ins)] Collecting load test artifacts"
    pwd
    ls -alh $output_dir
    find "$output_dir" -type f -name 'load-test.max-concurrency.json' -exec cp -vf {} "${ARTIFACT_DIR}" \;
    mkdir -p "${ARTIFACT_DIR}/iterations"
    find "$output_dir" -maxdepth 1 -type d -name 'iteration-*' -exec cp -vfr {} "${ARTIFACT_DIR}/iterations" \;
    mkdir -p "${ARTIFACT_DIR}/pprof"
    find "$output_dir" -type f -name '*.pprof' -exec cp -vf {} "${ARTIFACT_DIR}/pprof" \;
}

collect_monitoring_data() {
    echo "[$(date --utc -Ins)] Setting up tool to collect monitoring data"
    {
        python3 -m venv venv
        set +u
        # shellcheck disable=SC1091
        source venv/bin/activate
        set -u
        python3 -m pip install -U pip
        python3 -m pip install -e "git+https://github.com/redhat-performance/opl.git#egg=opl-rhcloud-perf-team-core&subdirectory=core"
    } &>"${ARTIFACT_DIR}/monitoring-setup.log"

    ## Monitoring data for entire test
    echo "[$(date --utc -Ins)] Collecting monitoring data for entire test"
    monitoring_collection_data="$ARTIFACT_DIR/load-test.max-concurrency.json"
    monitoring_collection_log="$ARTIFACT_DIR/monitoring-collection.log"
    monitoring_collection_dir="$ARTIFACT_DIR/monitoring-collection-raw-data-dir"
    mkdir -p "$monitoring_collection_dir"
    mstart=$(date --utc --date "$(status_data.py --status-data-file "$monitoring_collection_data" --get started)" --iso-8601=seconds)
    mend=$(date --utc --date "$(status_data.py --status-data-file "$monitoring_collection_data" --get ended)" --iso-8601=seconds)
    mhost=$(oc -n openshift-monitoring get route -l app.kubernetes.io/name=thanos-query -o json | jq --raw-output '.items[0].spec.host')
    status_data.py \
        --status-data-file "$monitoring_collection_data" \
        --additional ./tests/load-tests/ci-scripts/max-concurrency/cluster_read_config.yaml \
        --monitoring-start "$mstart" \
        --monitoring-end "$mend" \
        --monitoring-raw-data-dir "$monitoring_collection_dir" \
        --prometheus-host "https://$mhost" \
        --prometheus-port 443 \
        --prometheus-token "$(oc whoami -t)" \
        -d &>"$monitoring_collection_log"

    mkdir -p "$artifacts_ggm"
    for file in $(find "$monitoring_collection_dir/" -maxdepth 1 -name "*.csv"); do
        echo "Converting $file"
        out="$artifacts_ggm/$(basename "$file")"
        rm -rf "$out"
        while read line; do
            timestamp=$(echo "$line" | cut -d "," -f1)
            value=$(echo "$line" | cut -d "," -f2)
            echo "$(date -d "@$timestamp" "+%Y-%m-%dT%H:%M:%S.%N" --utc);$value" >>"$out"
        done <<<"$(tail -n +2 "$file")" &
    done
    wait

    ## Monitoring data per iteration
    for iteration_dir in $(find "$ARTIFACT_DIR/iterations/" -type d -name 'iteration-*'); do
        echo "[$(date --utc -Ins)] Collecting monitoring data for $iteration_dir"
        monitoring_collection_data="$iteration_dir/load-test.json"
        monitoring_collection_log="$iteration_dir/monitoring-collection.log"
        monitoring_collection_dir="$iteration_dir/monitoring-collection-raw-data-dir"
        if [[ ! -f "$monitoring_collection_data" ]]; then
            echo "[$(date --utc -Ins)] File $monitoring_collection_data missing, skipping $iteration_dir"
            continue
        fi
        mkdir -p "$monitoring_collection_dir"
        mstart=$(date --utc --date "$(status_data.py --status-data-file "$monitoring_collection_data" --get started)" --iso-8601=seconds)
        mend=$(date --utc --date "$(status_data.py --status-data-file "$monitoring_collection_data" --get ended)" --iso-8601=seconds)
        mhost=$(oc -n openshift-monitoring get route -l app.kubernetes.io/name=thanos-query -o json | jq --raw-output '.items[0].spec.host')
        status_data.py \
            --status-data-file "$monitoring_collection_data" \
            --additional ./tests/load-tests/cluster_read_config.yaml \
            --monitoring-start "$mstart" \
            --monitoring-end "$mend" \
            --monitoring-raw-data-dir "$monitoring_collection_dir" \
            --prometheus-host "https://$mhost" \
            --prometheus-port 443 \
            --prometheus-token "$(oc whoami -t)" \
            -d &>"$monitoring_collection_log"
    done

    set +u
    deactivate
    set -u
}

collect_tekton_profiling_data() {
    if [ "${TEKTON_PERF_ENABLE_CPU_PROFILING:-}" == "true" ] || [ "${TEKTON_PERF_ENABLE_MEMORY_PROFILING:-}" == "true" ]; then
        echo "[$(date --utc -Ins)] Collecting Tekton profiling data"
        for pprof_profile in $(find "$output_dir" -name "*.pprof"); do
            if [ -s "$pprof_profile" ]; then
                file=$(basename "$pprof_profile")
                go tool pprof -text "$pprof_profile" >"$artifact_pprof/$file.txt" || true
                go tool pprof -svg -output="$artifact_pprof/$file.svg" "$pprof_profile" || true
            fi
        done
    fi
}

_get_tekton_results_watcher_pod_count() {
    find "$output_dir" -type f -name 'tekton-results-watcher.tekton-results-watcher-*.goroutine-dump-0.0001-*.pprof' | wc -l
}

collect_scalability_data() {
    echo "[$(date --utc -Ins)] Collecting scalability data"

    tekton_results_watcher_pod_count=$(_get_tekton_results_watcher_pod_count)
    tekton_results_watcher_pod_headers=""
    for i in $(seq -w 1 "$tekton_results_watcher_pod_count"); do
        tekton_results_watcher_pod_headers="${tekton_results_watcher_pod_headers}${csv_delim}ParkedGoRoutinesPod$i"
    done

    max_concurrency_csv=$ARTIFACT_DIR/max-concurrency.csv
    echo "Iteration\
${csv_delim}Threads\
${csv_delim}WorkloadKPI\
${csv_delim}Errors\
${csv_delim}UserAvgTime\
${csv_delim}UserMaxTime\
${csv_delim}CreateApplicationAvgTime\
${csv_delim}CreateApplicationMaxTime\
${csv_delim}ValidateApplicationAvgTime\
${csv_delim}ValidateApplicationMaxTime\
${csv_delim}CreateComponentAvgTime\
${csv_delim}CreateComponentMaxTime\
${csv_delim}ValidatePipelineRunConditionAvgTime\
${csv_delim}ValidatePipelineRunConditionMaxTime\
${csv_delim}ValidatePipelineRunCreationAvgTime\
${csv_delim}ValidatePipelineRunCreationMaxTime\
${csv_delim}ValidatePipelineRunSignatureAvgTime\
${csv_delim}ValidatePipelineRunSignatureMaxTime\
${csv_delim}CreateIntegrationTestScenarioAvgTime\
${csv_delim}CreateIntegrationTestScenarioMaxTime\
${csv_delim}ValidateIntegrationTestScenarioAvgTime\
${csv_delim}ValidateIntegrationTestScenarioMaxTime\
${csv_delim}ValidateTestPipelineRunConditionAvgTime\
${csv_delim}ValidateTestPipelineRunConditionMaxTime\
${csv_delim}ValidateTestPipelineRunCreationAvgTime\
${csv_delim}ValidateTestPipelineRunCreationMaxTime\
${csv_delim}ClusterCPUUsageAvg\
${csv_delim}ClusterDiskUsageAvg\
${csv_delim}ClusterMemoryUsageAvg\
${csv_delim}ClusterPodCountAvg\
${csv_delim}ClusterNodesWorkerCountAvg\
${csv_delim}ClusterRunningPodsOnWorkersCountAvg\
${csv_delim}ClusterPVCInUseAvg\
${csv_delim}TektonResultsWatcherMemoryMin\
${csv_delim}TektonResultsWatcherMemoryMax\
${csv_delim}TektonResultsWatcherMemoryRange\
${csv_delim}TektonResultsWatcherCPUMin\
${csv_delim}TektonResultsWatcherCPUMax\
${csv_delim}TektonResultsWatcherCPURange\
${csv_delim}TektonResultsWatcherWorkqueueDepthMin\
${csv_delim}TektonResultsWatcherWorkqueueDepthMax\
${csv_delim}TektonResultsWatcherWorkqueueDepthRange\
${csv_delim}TektonResultsWatcherReconcileLatencyBucketMin\
${csv_delim}TektonResultsWatcherReconcileLatencyBucketMax\
${csv_delim}TektonResultsWatcherReconcileLatencyBucketRange\
${tekton_results_watcher_pod_headers}\
${csv_delim}SchedulerPendingPodsCountAvg\
${csv_delim}TokenPoolRatePrimaryAvg\
${csv_delim}TokenPoolRateSecondaryAvg\
${csv_delim}ClusterPipelineRunCountAvg\
${csv_delim}ClusterPipelineWorkqueueDepthAvg\
${csv_delim}ClusterPipelineScheduleFirstPodAvg\
${csv_delim}ClusterTaskRunThrottledByNodeResourcesAvg\
${csv_delim}ClusterTaskRunThrottledByDefinedQuotaAvg\
${csv_delim}EtcdRequestDurationSecondsAvg\
${csv_delim}ClusterNetworkBytesTotalAvg\
${csv_delim}ClusterNetworkReceiveBytesTotalAvg\
${csv_delim}ClusterNetworkTransmitBytesTotalAvg\
${csv_delim}NodeDiskIoTimeSecondsTotalAvg" \
        >"$max_concurrency_csv"
    iteration_dirs=$(find "$ARTIFACT_DIR/iterations" -type d -name 'iteration-*')
    if [ -n "$iteration_dirs" ]; then
        for iteration_dir in $iteration_dirs; do
            parked_go_routines=$(get_parked_go_routines "$iteration_dir")
            parked_go_routines_columns=""
            if [ -n "$parked_go_routines" ]; then
                for g in $parked_go_routines; do
                    parked_go_routines_columns="$parked_go_routines_columns + $csv_delim_quoted + \"$g\""
                done
            else
                for _ in $(seq 1 "$(_get_tekton_results_watcher_pod_count)"); do
                    parked_go_routines_columns="$parked_go_routines_columns + $csv_delim_quoted"
                done
            fi
            echo "[$(date --utc -Ins)] Processing $iteration_dir/load-test.json"
            jq -rc "(.metadata.\"max-concurrency\".iteration | tostring) \
                + $csv_delim_quoted + (.parameters.options.Concurrency | tostring) \
                + $csv_delim_quoted + (.results.measurements.KPI.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.KPI.errors | tostring) \
                + $csv_delim_quoted + (.results.measurements.HandleUser.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.HandleUser.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.createApplication.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.createApplication.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateApplication.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateApplication.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.createComponent.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.createComponent.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validatePipelineRunCondition.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validatePipelineRunCondition.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validatePipelineRunCreation.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validatePipelineRunCreation.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validatePipelineRunSignature.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validatePipelineRunSignature.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.createIntegrationTestScenario.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.createIntegrationTestScenario.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateIntegrationTestScenario.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateIntegrationTestScenario.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateTestPipelineRunCondition.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateTestPipelineRunCondition.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateTestPipelineRunCreation.pass.duration.mean | tostring) \
                + $csv_delim_quoted + (.results.measurements.validateTestPipelineRunCreation.pass.duration.max | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_cpu_usage_seconds_total_rate.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_disk_throughput_total.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_memory_usage_rss_total.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_pods_count.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_nodes_worker_count.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_running_pods_on_workers_count.mean | tostring) \
                + $csv_delim_quoted + (.measurements.storage_count_attachable_volumes_in_use.mean | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".\"container[watcher]\".memory.min | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".\"container[watcher]\".memory.max | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".\"container[watcher]\".memory.range | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".cpu.min | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".cpu.max | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".cpu.range | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".watcher_workqueue_depth.min | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".watcher_workqueue_depth.max | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".watcher_workqueue_depth.range | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".watcher_reconcile_latency_bucket.min | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".watcher_reconcile_latency_bucket.max | tostring) \
                + $csv_delim_quoted + (.measurements.\"tekton-results-watcher\".watcher_reconcile_latency_bucket.range | tostring) \
                ${parked_go_routines_columns} \
                + $csv_delim_quoted + (.measurements.scheduler_pending_pods_count.mean | tostring) \
                + $csv_delim_quoted + (.measurements.token_pool_rate_primary.mean | tostring) \
                + $csv_delim_quoted + (.measurements.token_pool_rate_secondary.mean | tostring) \
                + $csv_delim_quoted + (.measurements.tekton_pipelines_controller_running_pipelineruns_count.mean | tostring) \
                + $csv_delim_quoted + (.measurements.tekton_tekton_pipelines_controller_workqueue_depth.mean | tostring) \
                + $csv_delim_quoted + (.measurements.pipelinerun_duration_scheduled_seconds.mean | tostring) \
                + $csv_delim_quoted + (.measurements.tekton_pipelines_controller_running_taskruns_throttled_by_node.mean | tostring) \
                + $csv_delim_quoted + (.measurements.tekton_pipelines_controller_running_taskruns_throttled_by_quota.mean | tostring) \
                + $csv_delim_quoted + (.measurements.etcd_request_duration_seconds_average.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_network_bytes_total.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_network_receive_bytes_total.mean | tostring) \
                + $csv_delim_quoted + (.measurements.cluster_network_transmit_bytes_total.mean | tostring) \
                + $csv_delim_quoted + (.measurements.node_disk_io_time_seconds_total.mean | tostring)" \
                "$iteration_dir/load-test.json" >>"$max_concurrency_csv"
        done
    else
        echo "[$(date --utc -Ins)] WARNING: No file matching '$output_dir/load-test.max-concurrency.*.json' found!"
    fi
}

get_parked_go_routines() {
    goroutines_pprof=$(find "$1" -name "tekton-results-watcher.tekton-results-watcher-*.goroutine-dump-0.pprof")
    count=0
    for i in $goroutines_pprof; do
        if [ $count -gt 0 ]; then
            echo -n " "
        fi
        echo -n "$(go tool pprof -text "$i" 2>/dev/null | grep 'runtime.gopark$' | sed -e 's,[ ]*\([0-9]\+\) .*,\1,g')"
        count=$((count + 1))
    done
}

collect_timestamp_csvs() {
    echo "[$(date --utc -Ins)] Collecting PipelineRun timestamps"
    pipelinerun_timestamps=$ARTIFACT_DIR/pipelineruns.tekton.dev_timestamps.csv
    echo "PipelineRun${csv_delim}Namespace${csv_delim}Succeeded${csv_delim}Reason${csv_delim}Message${csv_delim}Created${csv_delim}Started${csv_delim}FinallyStarted${csv_delim}Completed${csv_delim}Created->Started${csv_delim}Started->FinallyStarted${csv_delim}FinallyStarted->Completed${csv_delim}SucceededDuration${csv_delim}FailedDuration" >"$pipelinerun_timestamps"
    jq_cmd=".items[] | (.metadata.name) \
+ $csv_delim_quoted + (.metadata.namespace) \
+ $csv_delim_quoted + (.status.conditions[0].status) \
+ $csv_delim_quoted + (.status.conditions[0].reason) \
+ $csv_delim_quoted + (.status.conditions[0].message|split($csv_delim_quoted)|join(\"_\")) \
+ $csv_delim_quoted + (.metadata.creationTimestamp) \
+ $csv_delim_quoted + (.status.startTime) \
+ $csv_delim_quoted + (.status.finallyStartTime) \
+ $csv_delim_quoted + (.status.completionTime) \
+ $csv_delim_quoted + (if .status.startTime != null and .metadata.creationTimestamp != null then ((.status.startTime | strptime($dt_format) | mktime) - (.metadata.creationTimestamp | strptime($dt_format) | mktime) | tostring) else \"\" end) \
+ $csv_delim_quoted + (if .status.finallyStartTime != null and .status.startTime != null then ((.status.finallyStartTime | strptime($dt_format) | mktime) - (.status.startTime | strptime($dt_format) | mktime) | tostring) else \"\" end) \
+ $csv_delim_quoted + (if .status.completionTime != null and .status.finallyStartTime != null then ((.status.completionTime | strptime($dt_format) | mktime) - (.status.finallyStartTime | strptime($dt_format) | mktime) | tostring) else \"\" end) \
+ $csv_delim_quoted + (if .status.conditions[0].status == \"True\" and .status.completionTime != null and .metadata.creationTimestamp != null then ((.status.completionTime | strptime($dt_format) | mktime) - (.metadata.creationTimestamp | strptime($dt_format) | mktime) | tostring) else \"\" end) \
+ $csv_delim_quoted + (if .status.conditions[0].status == \"False\" and .status.completionTime != null and .metadata.creationTimestamp != null then ((.status.completionTime | strptime($dt_format) | mktime) - (.metadata.creationTimestamp | strptime($dt_format) | mktime) | tostring) else \"\" end)"
    oc get pipelineruns.tekton.dev -A -o json | jq "$jq_cmd" | sed -e "s/\n//g" -e "s/^\"//g" -e "s/\"$//g" -e "s/Z;/;/g" | sort -t ";" -k 13 -r -n >>"$pipelinerun_timestamps"
}

echo "[$(date --utc -Ins)] Collecting max concurrency results"
jq_iso_8601_to_seconds="( \
    (if \$d | contains(\"m\") and (endswith(\"ms\") | not) then (\$d | capture(\"(?<minutes>\\\\d+)m(?<seconds>\\\\d+\\\\.?(\\\\d+)?)s\") | (.minutes | tonumber * 60) + (.seconds | tonumber)) else 0 end) + \
    (if \$d | (contains(\"m\") | not) and contains(\"s\") and (endswith(\"ms\") | not) and (endswith(\"µs\") | not) then (\$d | capture(\"(?<seconds>\\\\d+\\\\.\\\\d+)s\") | (.seconds | tonumber)) else 0 end) + \
    (if \$d | endswith(\"ms\") then (\$d | split(\"ms\") | .[0] | tonumber / 1000) else 0 end) + \
    (if \$d | endswith(\"µs\") then (\$d | split(\"µs\") | .[0] | tonumber / 1000000) else 0 end) \
) | tostring"

convert_go_duration_to_seconds() {
    local duration=$1
    local total_seconds=0

    # Extract hours, minutes, seconds, milliseconds, and microseconds
    if [[ $duration =~ ([0-9]*\.?[0-9]+)h ]]; then
        total_seconds=$(bc <<<"$total_seconds + ${BASH_REMATCH[1]} * 3600")
    fi
    if [[ $duration =~ ([0-9]*\.?[0-9]+)m ]]; then
        total_seconds=$(bc <<<"$total_seconds + ${BASH_REMATCH[1]} * 60")
    fi
    if [[ $duration =~ ([0-9]*\.?[0-9]+)s ]]; then
        total_seconds=$(bc <<<"$total_seconds + ${BASH_REMATCH[1]}")
    fi
    if [[ $duration =~ ([0-9]*\.?[0-9]+)ms ]]; then
        total_seconds=$(bc <<<"$total_seconds + ${BASH_REMATCH[1]} / 1000")
    fi
    if [[ $duration =~ ([0-9]*\.?[0-9]+)(µs|us) ]]; then
        total_seconds=$(bc <<<"$total_seconds + ${BASH_REMATCH[1]} / 1000000")
    fi

    echo $total_seconds
}

extract_ggm_from_logs() {
    iteration_dir=$1
    artifacts_ggm_workdir=$2
    log_file="$iteration_dir/tekton-results-api.logs"
    i=16
    output="$artifacts_ggm_workdir/$(basename "$log_file").ggmggm$i.csv"
    echo "Extracting GGMGGM$i data from $log_file into $output..."
    while read line; do
        duration_ts=$(echo "$line" | sed -e "s,.* time spent \([^ ]\+\) ts \([^ ]\+\) totalSize \(.*\),\2;\1;\3,g")
        IFS=";" read -ra tokens <<<"${duration_ts}"
        echo "$(date -d @"${tokens[0]}" --utc +"%Y-%m-%dT%H:%M:%S.%N");$(convert_go_duration_to_seconds ${tokens[1]});${tokens[2]}" >>"$output"
    done <<<"$(grep "GGMGGM$i" $log_file)"

    for i in 17 18; do
        output="$artifacts_ggm_workdir/$(basename "$log_file").ggmggm$i.csv"
        echo "Extracting GGMGGM$i data from $log_file into $output..."
        while read line; do
            duration_ts=$(echo "$line" | sed -e "s,.* count \([^ ]\+\) time \([^ ]\+\) ts \([^ ]\+\).*,\3;\2;\1,g")
            IFS=";" read -ra tokens <<<"${duration_ts}"
            echo "$(date -d @"${tokens[0]}" --utc +"%Y-%m-%dT%H:%M:%S.%N");$(convert_go_duration_to_seconds ${tokens[1]});${tokens[2]}" >>"$output"
        done <<<"$(grep "GGMGGM$i" $log_file)"
    done

    i=20
    output="$artifacts_ggm_workdir/$(basename "$log_file").ggmggm$i.csv"
    echo "Extracting GGMGGM$i data from $log_file into $output..."
    while read line; do
        duration_ts=$(echo "$line" | sed -e "s,.* runStream \([^ ]\+\) ts \([^ ]\+\),\2;\1,g")
        IFS=";" read -ra tokens <<<"${duration_ts}"
        echo "$(date -d @"${tokens[0]}" --utc +"%Y-%m-%dT%H:%M:%S.%N");$(convert_go_duration_to_seconds ${tokens[1]})" >>"$output"
    done <<<"$(grep "GGMGGM$i" $log_file)"

    for i in 24 25; do
        output="$artifacts_ggm_workdir/$(basename "$log_file").ggmggm$i.csv"
        echo "Extracting GGMGGM$i data from $log_file into $output..."
        while read line; do
            duration_ts=$(echo "$line" | sed -e "s,.* Write data \([^ ]\+\) ts \([^ ]\+\),\2;\1,g")
            IFS=";" read -ra tokens <<<"${duration_ts}"
            echo "$(date -d @"${tokens[0]}" --utc +"%Y-%m-%dT%H:%M:%S.%N");$(convert_go_duration_to_seconds ${tokens[1]})" >>"$output"
        done <<<"$(grep "GGMGGM$i" $log_file)"
    done

    i=31
    output="$artifacts_ggm_workdir/$(basename "$log_file").ggmggm$i.csv"
    echo "Extracting GGMGGM$i data from $log_file into $output..."
    while read line; do
        duration_ts=$(echo "$line" | sed -e "s,.* WriteStatus \([^ ]\+\) ts \([^ ]\+\),\2;\1,g")
        IFS=";" read -ra tokens <<<"${duration_ts}"
        echo "$(date -d @"${tokens[0]}" --utc +"%Y-%m-%dT%H:%M:%S.%N");$(convert_go_duration_to_seconds ${tokens[1]})" >>"$output"
    done <<<"$(grep "GGMGGM$i" $log_file)"

    i=33
    output="$artifacts_ggm_workdir/$(basename "$log_file").ggmggm$i.csv"
    echo "Extracting GGMGGM$i data from $log_file into $output..."
    while read line; do
        duration_ts=$(echo "$line" | sed -e "s,.* handleStream \([^ ]\+\) ts \([^ ]\+\),\2;\1,g")
        IFS=";" read -ra tokens <<<"${duration_ts}"
        echo "$(date -d @"${tokens[0]}" --utc +"%Y-%m-%dT%H:%M:%S.%N");$(convert_go_duration_to_seconds ${tokens[1]})" >>"$output"
    done <<<"$(grep "GGMGGM$i" $log_file)"
}

collect_tekton_results_logs() {
    echo "Collecting Tekton results logs..."
    mkdir -p "$artifacts_ggm"
    ts_format='"%Y-%m-%dT%H:%M:%S"'

    # jq_cmd="(.ts | strftime($ts_format)) + (.ts | tostring | capture(\".*(?<milliseconds>\\\\.\\\\d+)\") | .milliseconds) \
    #     + $csv_delim_quoted + ( \
    #         .msg | capture(\"(?<id>GGM(\\\\d+)?) (?<type>.+) kind (?<kind>\\\\S*) ns (?<ns>\\\\S*) name (?<name>\\\\S*).* times? spent (?<duration>.*)\") \
    #             | .id \
    #             + $csv_delim_quoted + (.type) \
    #             + $csv_delim_quoted + (.kind) \
    #             + $csv_delim_quoted + (.ns) \
    #             + $csv_delim_quoted + (.name) \
    #             + $csv_delim_quoted + (.duration) \
    #             + $csv_delim_quoted + (.duration as \$d | $jq_iso_8601_to_seconds ) \
    #         )"
    # component=tekton-results-api
    # metrics=("UpdateLog after handleReturn" "UpateLog after flush" "GRPC receive" "RBAC check" "get record" "create stream" "read stream")
    # for f in $(find $artifact_logs -type f -name "$component*.logs"); do
    #     echo "Processing $f..."
    #     grep "\"GGM" "$f" | sed -e 's,.*\({.*}\).*,\1,g' >$f.ggm.json
    #     jq -rc "$jq_cmd" $f.ggm.json >"$f.csv" || true
    #     for metric in "${metrics[@]}"; do
    #         m="$(echo "$metric" | sed -e 's,[ /],_,g')"
    #         grep "$metric"';' "$f.csv" >"$f.$m.csv"
    #     done &
    # done
    # wait
    # for metric in "${metrics[@]}"; do
    #     m="$(echo "$metric" | sed -e 's,[ /],_,g')"
    #     find "$artifact_logs" -name "$component.*.logs.$m.csv" | xargs cat | sort -u >"$ggm/$component.$m.csv"
    # done

    # component=tekton-results-watcher
    # metrics=("streamLogs" "dynamic Reconcile" "tkn read" "tkn write" "log copy and write" "flush" "close/rcv")
    # jq_cmd="if .ts | tostring | contains(\"-\") then .ts | capture(\"(?<t>.*)Z\") | .t else (.ts | strftime($ts_format)) + (.ts | tostring | capture(\".*(?<milliseconds>\\\\.\\\\d+)\") | .milliseconds) end \
    #     + ( \
    #         .msg | capture(\"(?<id>GGM(\\\\d+)?) (?<type>.+)(?<! obj)( obj)? kind (?<kind>\\\\S*) obj ns (?<ns>\\\\S*) obj name (?<name>\\\\S*) times? spent (?<duration>.*)\") \
    #             | $csv_delim_quoted + (.id) \
    #             + $csv_delim_quoted + (.type) \
    #             + $csv_delim_quoted + (.kind) \
    #             + $csv_delim_quoted + (.ns) \
    #             + $csv_delim_quoted + (.name) \
    #             + $csv_delim_quoted + (.duration) \
    #             + $csv_delim_quoted + (.duration as \$d | $jq_iso_8601_to_seconds ) \
    #         )"
    # for f in $(find $artifact_logs -type f -name "$component*.logs"); do
    #     echo "Processing $f..."
    #     grep "\"GGM" "$f" | sed -e 's,.*\({.*}\).*,\1,g' >$f.ggm.json
    #     jq -rc "$jq_cmd" $f.ggm.json >"$f.csv" || true
    #     for metric in "${metrics[@]}"; do
    #         m="$(echo "$metric" | sed -e 's,[ /],_,g')"
    #         grep "$metric"';' "$f.csv" >"$f.$m.csv"
    #     done &
    # done
    # wait
    # for metric in "${metrics[@]}"; do
    #     m="$(echo "$metric" | sed -e 's,[ /],_,g')"
    #     find "$artifact_logs" -name "$component.*.logs.$m.csv" | xargs cat | sort -u >"$ggm/$component.$m.csv"
    # done

    # GGMGGMXX

    artifacts_ggm_workdir=$artifacts_ggm/workdir
    rm -rvf "$artifacts_ggm_workdir"
    mkdir -p "$artifacts_ggm_workdir"
    for iteration_dir in $(find "$output_dir" -type d -name "iteration-*"); do
        extract_ggm_from_logs "$iteration_dir" "$artifacts_ggm_workdir" &
    done
    wait

    for ggm_csv in $(find "$artifacts_ggm_workdir" -name "*.csv"); do
        output="$artifacts_ggm/$(basename "$ggm_csv")"
        echo "Defragmenting $ggm_csv -> $output"
        sort -u <"$ggm_csv" >"$output"
    done
    rm -rf "$artifacts_ggm_workdir"
}

echo "Collecting max concurrency results..."
collect_artifacts || true
collect_tekton_results_logs || true
collect_timestamp_csvs || true
collect_monitoring_data || true
collect_scalability_data || true
collect_tekton_profiling_data || true
popd
