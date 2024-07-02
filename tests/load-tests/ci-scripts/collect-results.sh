#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

# shellcheck disable=SC1090
source "/usr/local/ci-secrets/redhat-appstudio-load-test/load-test-scenario.${1:-concurrent}"
source "$(dirname $0)/utils.sh"
source "$(dirname $0)/user-prefix.sh"

echo "[$(date --utc -Ins)] Collecting load test results"

# Setup directories
ARTIFACT_DIR=${ARTIFACT_DIR:-artifacts}
mkdir -p ${ARTIFACT_DIR}
pushd "${2:-./tests/load-tests}"

echo "[$(date --utc -Ins)] Collecting artifacts"
find . -maxdepth 1 -type f -name '*.log' -exec cp -vf {} "${ARTIFACT_DIR}" \;
find . -maxdepth 1 -type f -name '*.csv' -exec cp -vf {} "${ARTIFACT_DIR}" \;
find . -maxdepth 1 -type f -name 'load-test-options.json' -exec cp -vf {} "${ARTIFACT_DIR}" \;
find . -maxdepth 1 -type d -name 'collected-data' -exec cp -r {} "${ARTIFACT_DIR}" \;

echo "[$(date --utc -Ins)] Setting up Python venv"
{
    python3 -m venv venv
    set +u
    source venv/bin/activate
    set -u
    python3 -m pip install -U pip
    python3 -m pip install -e "git+https://github.com/redhat-performance/opl.git#egg=opl-rhcloud-perf-team-core&subdirectory=core"
    python3 -m pip install tabulate
    python3 -m pip install matplotlib
    python3 -m pip install 'tenacity<8.4.0'
} &>"${ARTIFACT_DIR}/monitoring-setup.log"

echo "[$(date --utc -Ins)] Create summary JSON with timings"
./evaluate.py "${ARTIFACT_DIR}/load-test-timings.csv" "${ARTIFACT_DIR}/load-test-timings.json"

echo "[$(date --utc -Ins)] Counting PRs and TRs"
ci-scripts/utility_scripts/count-multiarch-taskruns.py --data-dir "${ARTIFACT_DIR}" >"${ARTIFACT_DIR}/count-multiarch-taskruns.log"

echo "[$(date --utc -Ins)] Graphing PRs and TRs"
ci-scripts/utility_scripts/show-pipelineruns.py --data-dir "${ARTIFACT_DIR}" >"${ARTIFACT_DIR}/show-pipelineruns.log"
mv "${ARTIFACT_DIR}/output.svg" "${ARTIFACT_DIR}/show-pipelines.svg"

echo "[$(date --utc -Ins)] Creating main status data file"
STATUS_DATA_FILE="${ARTIFACT_DIR}/load-test.json"
status_data.py \
    --status-data-file "${STATUS_DATA_FILE}" \
    --set "name=Konflux loadtest" "started=$(cat started)" "ended=$(cat ended)" \
    --set-subtree-json "parameters.options=${ARTIFACT_DIR}/load-test-options.json" "results.measurements=${ARTIFACT_DIR}/load-test-timings.json"

echo "[$(date --utc -Ins)] Adding monitoring data"
mstarted="$(date -d "$(cat started)" --utc -Iseconds)"
mended="$(date -d "$(cat ended)" --utc -Iseconds)"
mhost="https://$(oc -n openshift-monitoring get route -l app.kubernetes.io/name=thanos-query -o json | jq --raw-output '.items[0].spec.host')"
mrawdir="${ARTIFACT_DIR}/monitoring-raw-data-dir/"
mkdir -p "$mrawdir"
status_data.py \
    --status-data-file "${STATUS_DATA_FILE}" \
    --additional cluster_read_config.yaml \
    --monitoring-start "$mstarted" \
    --monitoring-end "$mended" \
    --prometheus-host "$mhost" \
    --prometheus-port 443 \
    --prometheus-token "$(oc whoami -t)" \
    --monitoring-raw-data-dir "$mrawdir" \
    &>"${ARTIFACT_DIR}/monitoring-collection.log"

deactivate

echo "[$(date --utc -Ins)] Collecting additional info"
application_stub=$ARTIFACT_DIR/collected-applications.appstudio.redhat.com
component_stub=$ARTIFACT_DIR/collected-components.appstudio.redhat.com
node_stub=$ARTIFACT_DIR/collected-nodes

## Application info
echo "Collecting Application timestamps..."
collect_application "-A" "$application_stub"

## Component info
echo "Collecting Component timestamps..."
collect_component "-A" "$component_stub"

## Nodes info
#echo "Collecting node specs"
#collect_nodes "$node_stub"

## Application service log segments per user app
echo "[$(date --utc -Ins)] Collecting application service log segments per user app"
application_service_log=$ARTIFACT_DIR/application-service.log
application_service_log_segments=$ARTIFACT_DIR/application-service-log-segments
oc logs -l "control-plane=controller-manager" --tail=-1 -n application-service >"$application_service_log"
mkdir -p "$application_service_log_segments"
for i in $(grep -Eo "${USER_PREFIX}-....-app" "$application_service_log" | sort | uniq); do grep "$i" "$application_service_log" >"$application_service_log_segments/$i.log"; done

## Collect Tekton profiling data
if [ "${TEKTON_PERF_ENABLE_CPU_PROFILING:-}" == "true" ] || [ "${TEKTON_PERF_ENABLE_MEMORY_PROFILING:-}" == "true" ]; then
    echo "[$(date --utc -Ins)] Collecting profiling data from Tekton"
    for pprof_profile in $(find . -name "*.pprof"); do
        file=$(basename "$pprof_profile")
        go tool pprof -text "$pprof_profile" >"$ARTIFACT_DIR/$file.txt" || true
        go tool pprof -svg -output="$ARTIFACT_DIR/$file.svg" "$pprof_profile" || true
    done
fi

#echo "[$(date --utc -Ins)] Installing Tekton Artifact Performance Analysis (tapa)"
#{
#tapa_dir=./tapa.git
#rm -rf "$tapa_dir"
#git clone https://github.com/gabemontero/tekton-artifact-performance-analysis "$tapa_dir"
#pushd "$tapa_dir"
#go mod tidy
#go mod vendor
#go build -o tapa . && chmod +x ./tapa
#popd
#} &>"${ARTIFACT_DIR}/tapa-installation.log"
#export PATH="$PATH:$tapa_dir"
#
#tapa="tapa -t csv"
#
#echo "[$(date --utc -Ins)] Running Tekton Artifact Performance Analysis"
#tapa_prlist_csv=$ARTIFACT_DIR/tapa.prlist.csv
#tapa_trlist_csv=$ARTIFACT_DIR/tapa.trlist.csv
#tapa_podlist_csv=$ARTIFACT_DIR/tapa.podlist.csv
#tapa_podlist_containers_csv=$ARTIFACT_DIR/tapa.podlist.containers.csv
#tapa_all_csv=$ARTIFACT_DIR/tapa.all.csv
#tapa_tmp=tapa.tmp
#
#sort_csv() {
#    if [ -f "$1" ]; then
#        head -n1 "$1" >"$2"
#        tail -n+2 "$1" | sort -t ";" -k 2 -r -n >>"$2"
#    else
#        echo "WARNING: File $1 not found!"
#    fi
#}
#
#$tapa prlist "${pipelinerun_stub}.json" >"$tapa_tmp"
#sort_csv "$tapa_tmp" "$tapa_prlist_csv"
#
#$tapa trlist "${taskrun_stub}.json" >"$tapa_tmp"
#sort_csv "$tapa_tmp" "$tapa_trlist_csv"
#
#$tapa podlist "${pod_stub}.json" >"$tapa_tmp"
#sort_csv "$tapa_tmp" "$tapa_podlist_csv"
#
#$tapa podlist --containers-only "${pod_stub}.json" >"$tapa_tmp"
#sort_csv "$tapa_tmp" "$tapa_podlist_containers_csv"
#
#$tapa all "${pipelinerun_stub}.json" "${taskrun_stub}.json" "${pod_stub}.json" >"$tapa_tmp"
#sort_csv "$tapa_tmp" "$tapa_all_csv"

popd
