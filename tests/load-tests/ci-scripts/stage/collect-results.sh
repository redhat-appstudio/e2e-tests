#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

source "$(dirname $0)/../utils.sh"

echo "[$(date --utc -Ins)] Collecting load test results"

# Setup directories
ARTIFACT_DIR=${ARTIFACT_DIR:-.artifacts}
CONCURRENCY="${1:-1}"
mkdir -p ${ARTIFACT_DIR}
pushd "${2:-./tests/load-tests}"

# Construct $PROMETHEUS_HOST by extracting BASE_URL from $STAGE_MEMBER_CLUSTER
BASE_URL=$(echo $STAGE_MEMBER_CLUSTER | grep -oP 'https://api\.\K[^:]+')
PROMETHEUS_HOST="thanos-querier-openshift-monitoring.apps.$BASE_URL"
TOKEN=${OCP_PROMETHEUS_TOKEN}

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
ci-scripts/utility_scripts/show-pipelineruns.py --data-dir "${ARTIFACT_DIR}" >"${ARTIFACT_DIR}/show-pipelineruns.log" || true
mv "${ARTIFACT_DIR}/output.svg" "${ARTIFACT_DIR}/show-pipelines.svg" || true

echo "[$(date --utc -Ins)] Creating main status data file"
STATUS_DATA_FILE="${ARTIFACT_DIR}/load-test.json"
status_data.py \
    --status-data-file "${STATUS_DATA_FILE}" \
    --set "name=Konflux loadtest" "started=$(cat started)" "ended=$(cat ended)" \
    --set-subtree-json "parameters.options=${ARTIFACT_DIR}/load-test-options.json" "results.measurements=${ARTIFACT_DIR}/load-test-timings.json"

echo "[$(date --utc -Ins)] Adding monitoring data"
mstarted="$(date -d "$(cat started)" --utc -Iseconds)"
mended="$(date -d "$(cat ended)" --utc -Iseconds)"
mhost="https://$PROMETHEUS_HOST"
mrawdir="${ARTIFACT_DIR}/monitoring-raw-data-dir/"
mkdir -p "$mrawdir"
status_data.py \
    --status-data-file "${STATUS_DATA_FILE}" \
    --additional ci-scripts/stage/cluster_read_config.yaml \
    --monitoring-start "$mstarted" \
    --monitoring-end "$mended" \
    --prometheus-host "$mhost" \
    --prometheus-port 443 \
    --prometheus-token "$TOKEN" \
    --monitoring-raw-data-dir "$mrawdir" \
    &>"${ARTIFACT_DIR}/monitoring-collection.log"

deactivate

echo "[$(date --utc -Ins)] Collecting additional info"
if ! [ -r users.json ]; then
    echo "ERROR: Missing file with user creds"
else
    login_log_stub="${ARTIFACT_DIR}/collected-oc_login"
    application_stub="${ARTIFACT_DIR}/collected-applications.appstudio.redhat.com"
    component_stub="${ARTIFACT_DIR}/collected-components.appstudio.redhat.com"

    for uid in $(seq 1 $CONCURRENCY); do
        username="test-rhtap-$uid"
        offline_token=$(cat users.json | jq --raw-output '.[] | select(.username == "'$username'").token')
        api_server=$(cat users.json | jq --raw-output '.[] | select(.username == "'$username'").apiurl')
        sso_server=$(cat users.json | jq --raw-output '.[] | select(.username == "'$username'").ssourl')
        access_token=$(curl \
            --silent \
            --header "Accept: application/json" \
            --header "Content-Type: application/x-www-form-urlencoded" \
            --data-urlencode "grant_type=refresh_token" \
            --data-urlencode "client_id=cloud-services" \
            --data-urlencode "refresh_token=${offline_token}" \
            "${sso_server}" |
            jq --raw-output ".access_token")
        login_log="${login_log_stub}-${username}.log"
        echo "Logging in as $username..."
        if ! oc login --token="$access_token" --server="$api_server" &>$login_log; then
            echo "ERROR: Login as $username failed:"
            cat "$login_log"
            continue
        fi
        tenant="${username}-tenant"

        # Application info
        echo "Collecting Application timestamps..."
        collect_application "-n ${tenant}" "$application_stub-$tenant"

        # Component info
        echo "Collecting Component timestamps..."
        collect_component "-n ${tenant}" "$component_stub-$tenant"
    done
fi

popd
