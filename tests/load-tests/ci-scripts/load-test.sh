#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

# shellcheck disable=SC1090
source "/usr/local/ci-secrets/redhat-appstudio-load-test/load-test-scenario.${1:-concurrent}"
source "$( dirname $0 )/user-prefix.sh"

pushd "${2:-./tests/load-tests}"

source "./ci-scripts/user-prefix.sh"

export QUAY_E2E_ORGANIZATION MY_GITHUB_ORG GITHUB_USER GITHUB_TOKEN TEKTON_PERF_ENABLE_PROFILING TEKTON_PERF_ENABLE_CPU_PROFILING TEKTON_PERF_ENABLE_MEMORY_PROFILING TEKTON_PERF_PROFILE_CPU_PERIOD KUBE_SCHEDULER_LOG_LEVEL
QUAY_E2E_ORGANIZATION=$(cat /usr/local/ci-secrets/redhat-appstudio-load-test/quay-org)
MY_GITHUB_ORG=$(cat /usr/local/ci-secrets/redhat-appstudio-load-test/github-org)

rate_limits_csv="${OUTPUT_DIR:-.}/gh-rate-limits-remaining.csv"

echo "Starting a watch for GH rate limits remainig"
IFS="," read -ra kvs <<<"$(cat /usr/local/ci-secrets/redhat-appstudio-load-test/github_accounts)"
echo -n "Time" >"$rate_limits_csv"
for kv in "${kvs[@]}"; do
    IFS=":" read -ra name_token <<<"$kv"
    echo -n ";${name_token[0]}" >>"$rate_limits_csv"
done
echo >>"$rate_limits_csv"

echo -e "[INFO] Start tests with user: ${GITHUB_USER}"

while true; do
    timestamp=$(printf "%s" "$(date -u +'%FT%T')")
    echo -n "$timestamp" >>"$rate_limits_csv"
    for kv in "${kvs[@]}"; do
        IFS=":" read -ra name_token <<<"$kv"
        rate=$(curl -s -H "Accept: application/vnd.github+json" -H "Authorization: token ${name_token[1]}" -H "X-GitHub-Api-Version: 2022-11-28" 'https://api.github.com/rate_limit' | jq -rc '(.rate.remaining|tostring)')
        echo -n ";$rate" >>"$rate_limits_csv"
    done
    echo >>"$rate_limits_csv"
    sleep 10s
done &

rate_limit_exit=$!
kill_rate_limits() {
    echo "Stopping the watch for GH rate limits remainig"
    kill $rate_limit_exit
}
trap kill_rate_limits EXIT

./run.sh

popd
