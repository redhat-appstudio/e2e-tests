#!/bin/bash
# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u

echo '[INFO] Deploying SPI OAuth2 config'

export OAUTH_URL='spi-oauth-route-spi-system.'$( oc get ingresses.config/cluster -o jsonpath={.spec.domain})
export tmpfile=$(mktemp -d)/config.yaml

# We are injecting the token manually for the e2e. No need a real secret for now
export SPI_GITHUB_CLIENT_ID="app-client-id"
export SPI_GITHUB_CLIENT_SECRET="app-secret"

spiConfig=$(cat <<EOF
sharedSecret: $(openssl rand -hex 20)
serviceProviders:
  - type: GitHub
    clientId: $SPI_GITHUB_CLIENT_ID
    clientSecret: $SPI_GITHUB_CLIENT_SECRET
baseUrl: https://spi-oauth-route-spi-system.$( oc get ingresses.config/cluster -o jsonpath={.spec.domain})
EOF
)

echo "Please go to https://github.com/settings/developers."
echo "And register new Github OAuth application for callback https://"$OAUTH_URL"/github/callback"

echo "$spiConfig" > "$tmpfile"
oc create namespace spi-system --dry-run=client -o yaml | oc apply -f -
oc create secret generic oauth-config \
    --save-config --dry-run=client \
    --from-file="$tmpfile" \
    -n spi-system \
    -o yaml |
oc apply -f -

rm "$tmpfile"

# The env var NAMESPACE is exported by openshift-ci and breaks the vault-init script. It has to be set to an empty string.
curl https://raw.githubusercontent.com/redhat-appstudio/service-provider-integration-operator/main/hack/vault-init.sh | NAMESPACE="" bash -s

oc rollout restart  deployment/spi-controller-manager -n spi-system
oc rollout restart  deployment/spi-oauth-service -n spi-system
