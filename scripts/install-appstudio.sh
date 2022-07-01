#!/bin/bash
#
# Download gitops repository to install AppStudio in e2e mode.
#
# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u

# Mandatory env vars defined in local environment/CI
export MY_GITHUB_TOKEN="${GITHUB_TOKEN}"

# Optionally provided env vars
export MY_GITHUB_ORG=${GITHUB_E2E_ORGANIZATION:-"redhat-appstudio-qe"}
export MY_GIT_FORK_REMOTE="qe"
export E2E_APPLICATIONS_NAMESPACE=${E2E_APPLICATIONS_NAMESPACE:-appstudio-e2e-test}
export SHARED_SECRET_NAMESPACE="build-templates"
export TMP_DIR TEST_BRANCH_ID ROOT_E2E
TMP_DIR=$(mktemp -d)
TEST_BRANCH_ID=$(date +%s)
# Environment variable used to override the default "protected" image repository in HAS
# https://github.com/redhat-appstudio/application-service/blob/6b9d21b8f835263b2e92f1e9343a1453caa2e561/gitops/generate_build.go#L50
# Users are allowed to push images to this repo only in case the image contains a tag that consists of "<USER'S_NAMESPACE_NAME>-<CUSTOM-TAG>"
# For example: "quay.io/redhat-appstudio-qe/test-images-protected:appstudio-e2e-test-mytag123"
export HAS_DEFAULT_IMAGE_REPOSITORY="quay.io/${QUAY_E2E_ORGANIZATION:-redhat-appstudio-qe}/test-images-protected"


pushd "${TMP_DIR}"

INFRA_DEPLOYMENTS_ORG="${INFRA_DEPLOYMENTS_ORG:-"redhat-appstudio"}"
INFRA_DEPLOYMENTS_BRANCH="${INFRA_DEPLOYMENTS_BRANCH:-"main"}"
git clone --no-checkout "https://${MY_GITHUB_TOKEN}@github.com/${INFRA_DEPLOYMENTS_ORG}/infra-deployments.git" .
git checkout "${INFRA_DEPLOYMENTS_BRANCH}"
# Add a custom remote for infra-deployments repository.
git remote add "${MY_GIT_FORK_REMOTE}" https://github.com/"${MY_GITHUB_ORG}"/infra-deployments.git
# Run the bootstrap script
./hack/bootstrap-cluster.sh preview
# Secret used by pipelines to push component containers to quay.io
QUAY_TOKEN=${QUAY_TOKEN:-}
echo -e "[INFO] Creating application-service related secret in $SHARED_SECRET_NAMESPACE namespace"
echo "$QUAY_TOKEN" | base64 --decode > docker.config
kubectl create secret docker-registry redhat-appstudio-user-workload -n $SHARED_SECRET_NAMESPACE --from-file=.dockerconfigjson=docker.config || true
rm docker.config

popd