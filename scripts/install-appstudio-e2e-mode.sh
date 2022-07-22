#!/bin/bash
#
# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u

command -v kubectl >/dev/null 2>&1 || { echo "kubectl is not installed. Aborting."; exit 1; }
command -v oc >/dev/null 2>&1 || { echo "oc cli is not installed. Aborting."; exit 1; }

export MY_GIT_FORK_REMOTE="qe"
export MY_GITHUB_ORG=${GITHUB_E2E_ORGANIZATION:-"redhat-appstudio-qe"}
export MY_GITHUB_TOKEN="${GITHUB_TOKEN}"
export TEST_BRANCH_ID=$(date +%s)
export ROOT_E2E="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"/..
export WORKSPACE=${WORKSPACE:-${ROOT_E2E}}
export E2E_APPLICATIONS_NAMESPACE=${E2E_APPLICATIONS_NAMESPACE:-appstudio-e2e-test}
export SHARED_SECRET_NAMESPACE="build-templates"

# Environment variable used to override the default "protected" image repository in HAS
# https://github.com/redhat-appstudio/application-service/blob/6b9d21b8f835263b2e92f1e9343a1453caa2e561/gitops/generate_build.go#L50
# Users are allowed to push images to this repo only in case the image contains a tag that consists of "<USER'S_NAMESPACE_NAME>-<CUSTOM-TAG>"
# For example: "quay.io/redhat-appstudio-qe/test-images-protected:appstudio-e2e-test-mytag123"
export HAS_DEFAULT_IMAGE_REPOSITORY="quay.io/${QUAY_E2E_ORGANIZATION:-redhat-appstudio-qe}/test-images-protected"

# Path to install openshift-ci tools
export PATH=$PATH:/tmp/bin
mkdir -p /tmp/bin

function installCITools() {
    curl -H "Authorization: token $GITHUB_TOKEN" -LO https://github.com/mikefarah/yq/releases/download/v4.20.2/yq_linux_amd64 && \
    chmod +x ./yq_linux_amd64 && \
    mv ./yq_linux_amd64 /tmp/bin/yq && \
    yq --version
}

# Download gitops repository to install AppStudio in e2e mode.
function cloneInfraDeployments() {
    git clone https://$GITHUB_TOKEN@github.com/redhat-appstudio/infra-deployments.git "$WORKSPACE"/tmp/infra-deployments
}

# Add a custom remote for infra-deployments repository.
function addQERemoteForkAndInstallAppstudio() {
    cd "$WORKSPACE"/tmp/infra-deployments
    git remote add "${MY_GIT_FORK_REMOTE}" https://github.com/"${MY_GITHUB_ORG}"/infra-deployments.git

    # Start AppStudio installation
    /bin/bash hack/bootstrap-cluster.sh preview
    cd "$WORKSPACE"
}

# Add a custom remote for infra-deployments repository.
function initializeSPIVault() {
   curl https://raw.githubusercontent.com/redhat-appstudio/e2e-tests/main/scripts/spi-e2e-setup.sh | bash -s
}

# Secrets used by pipelines to push component containers to quay.io
function createApplicationServiceSecrets() {
    echo -e "[INFO] Creating application-service related secrets in $SHARED_SECRET_NAMESPACE namespace"

    echo "$QUAY_TOKEN" | base64 --decode > docker.config
    kubectl create secret docker-registry redhat-appstudio-user-workload -n $SHARED_SECRET_NAMESPACE --from-file=.dockerconfigjson=docker.config || true
    rm docker.config
}

while [[ $# -gt 0 ]]
do
    case "$1" in
        install)
            installCITools
            cloneInfraDeployments
            addQERemoteForkAndInstallAppstudio
            createApplicationServiceSecrets
            initializeSPIVault
            ;;
        *)
            ;;
    esac
    shift
done
