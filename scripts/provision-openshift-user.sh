#!/bin/bash
#
# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u

export TMP_CI_USER="appstudioci"
export CONTEXT=$(oc config current-context)
export OC_CLUSTER=$(oc config view -ojsonpath="{.contexts[?(@.name == \"$CONTEXT\")].context.cluster}")
export API_SERVER=$(oc config view -ojsonpath="{.clusters[?(@.name == \"$OC_CLUSTER\")].cluster.server}")
export KUBECONFIG_TEST=${KUBECONFIG_TEST:-"/tmp/kubeconfig"}

cat <<EOF | oc apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: htpass-secret
  namespace: openshift-config
data: 
  htpasswd: YXBwc3R1ZGlvY2k6JDJ5JDA1JEF3M0k4TFIyemVROG8yazBrb1d2dXVDSmRwL3F5ZkJLdnp0cks4MFZveEpiZFJvQlAxYy51
EOF

oc patch oauths cluster --type merge -p '
spec:
  identityProviders:
    - name: htpasswd
      mappingMethod: claim
      type: HTPasswd
      htpasswd:
        fileData:
          name: htpass-secret
'

oc adm policy add-cluster-role-to-user cluster-admin appstudioci

function waitForNewOCPLogin() {
    while ! oc login --kubeconfig="${KUBECONFIG_TEST}" --server $API_SERVER --username="${TMP_CI_USER}" --password=${TMP_CI_USER} --insecure-skip-tls-verify; do
        sleep 20
    done
}

export -f waitForNewOCPLogin
timeout --foreground 10m bash -c waitForNewOCPLogin
