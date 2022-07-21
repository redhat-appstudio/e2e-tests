#!/bin/bash

# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u

export WORKSPACE=$(dirname $(dirname $(readlink -f "$0")));
export APPLICATION_NAMESPACE="openshift-gitops"
export APPLICATION_NAME="all-components-staging"

# Available openshift ci environments https://docs.ci.openshift.org/docs/architecture/step-registry/#available-environment-variables
export ARTIFACTS_DIR=${ARTIFACT_DIR:-"/tmp/appstudio"}

function waitHASApplicationToBeReady() {
    while [ "$(kubectl get applications.argoproj.io has -n openshift-gitops -o jsonpath='{.status.health.status}')" != "Healthy" ]; do
        sleep 30s
        echo "[INFO] Waiting for HAS to be ready."
    done
}

function waitAppStudioToBeReady() {
    while [ "$(kubectl get applications.argoproj.io ${APPLICATION_NAME} -n ${APPLICATION_NAMESPACE} -o jsonpath='{.status.health.status}')" != "Healthy" ] ||
          [ "$(kubectl get applications.argoproj.io ${APPLICATION_NAME} -n ${APPLICATION_NAMESPACE} -o jsonpath='{.status.sync.status}')" != "Synced" ]; do
        sleep 1m
        echo "[INFO] Waiting for AppStudio to be ready."
    done
}

function waitBuildToBeReady() {
    while [ "$(kubectl get applications.argoproj.io build -n ${APPLICATION_NAMESPACE} -o jsonpath='{.status.health.status}')" != "Healthy" ] ||
          [ "$(kubectl get applications.argoproj.io build -n ${APPLICATION_NAMESPACE} -o jsonpath='{.status.sync.status}')" != "Synced" ]; do
        sleep 1m
        echo "[INFO] Waiting for Build to be ready."
    done
}

function waitSPIToBeReady() {
    while [ "$(kubectl get applications.argoproj.io spi -n ${APPLICATION_NAMESPACE} -o jsonpath='{.status.health.status}')" != "Healthy" ] ||
          [ "$(kubectl get applications.argoproj.io spi -n ${APPLICATION_NAMESPACE} -o jsonpath='{.status.sync.status}')" != "Synced" ]; do
        sleep 1m
        echo "[INFO] Waiting for spi to be ready."
    done
}

function executeE2ETests() {
    ginkgo -p "${WORKSPACE}"/cmd --junit-report="${ARTIFACTS_DIR}"/e2e-report.xml --progress --vv -- --config-suites="${WORKSPACE}"/tests/e2e-demos/config/default.yaml
}

go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo@latest
go mod tidy -compat=1.17
go mod vendor
ginkgo version

# Initiate openshift ci users
export KUBECONFIG_TEST="/tmp/kubeconfig"
/bin/bash "$WORKSPACE"/scripts/provision-openshift-user.sh

export KUBECONFIG="${KUBECONFIG_TEST}"

/bin/bash "$WORKSPACE"/scripts/install-appstudio-e2e-mode.sh install

export -f waitAppStudioToBeReady
export -f waitBuildToBeReady
export -f waitHASApplicationToBeReady
export -f waitSPIToBeReady

# Install AppStudio Controllers and wait for HAS and other AppStudio application to be running.
timeout --foreground 10m bash -c waitAppStudioToBeReady
timeout --foreground 10m bash -c waitBuildToBeReady
timeout --foreground 10m bash -c waitHASApplicationToBeReady
timeout --foreground 10m bash -c waitSPIToBeReady

executeE2ETests
