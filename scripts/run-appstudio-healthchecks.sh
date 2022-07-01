#!/bin/bash

# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u

export GITOPS_NAMESPACE="openshift-gitops"
export GITOPS_ALL_APPS_CR_NAME="all-components-staging"

function waitHASApplicationToBeReady() {
    while [ "$(kubectl get applications.argoproj.io has -n ${GITOPS_NAMESPACE}  -o jsonpath='{.status.health.status}')" != "Healthy" ]; do
        sleep 30
        echo "[INFO] Waiting for HAS to be ready."
    done
}

function waitAppStudioToBeReady() {
    while [ "$(kubectl get applications.argoproj.io ${GITOPS_ALL_APPS_CR_NAME} -n ${GITOPS_NAMESPACE} -o jsonpath='{.status.health.status}')" != "Healthy" ] ||
          [ "$(kubectl get applications.argoproj.io ${GITOPS_ALL_APPS_CR_NAME} -n ${GITOPS_NAMESPACE} -o jsonpath='{.status.sync.status}')" != "Synced" ]; do
        sleep 30
        echo "[INFO] Waiting for AppStudio to be ready."
    done
}

function waitBuildToBeReady() {
    while [ "$(kubectl get applications.argoproj.io build -n ${GITOPS_NAMESPACE} -o jsonpath='{.status.health.status}')" != "Healthy" ] ||
          [ "$(kubectl get applications.argoproj.io build -n ${GITOPS_NAMESPACE} -o jsonpath='{.status.sync.status}')" != "Synced" ]; do
        sleep 30
        echo "[INFO] Waiting for Build to be ready."
    done
}

function waitSPIToBeReady() {
    while [ "$(kubectl get applications.argoproj.io spi -n ${GITOPS_NAMESPACE} -o jsonpath='{.status.health.status}')" != "Healthy" ] ||
          [ "$(kubectl get applications.argoproj.io spi -n ${GITOPS_NAMESPACE} -o jsonpath='{.status.sync.status}')" != "Synced" ]; do
        echo "[INFO] Waiting for spi to be ready."
        sleep 30
    done
}


export -f waitAppStudioToBeReady
export -f waitBuildToBeReady
export -f waitHASApplicationToBeReady
export -f waitSPIToBeReady

# Install AppStudio Controllers and wait for HAS and other AppStudio application to be running.
timeout --foreground 10m bash -c waitAppStudioToBeReady
timeout --foreground 10m bash -c waitBuildToBeReady
timeout --foreground 10m bash -c waitHASApplicationToBeReady
timeout --foreground 10m bash -c waitSPIToBeReady
