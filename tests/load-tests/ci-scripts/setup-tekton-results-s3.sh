#!/bin/bash

if [ -z "${TEKTON_RESULTS_S3_BUCKET_NAME}" ]; then
    echo "TEKTON_RESULTS_S3_BUCKET_NAME env variable is not set or empty - skipping setting up Tekton Results to use S3"
else
    echo "Setting up Tekton Results to use S3"
fi

export AWS_REGION=$(cat /usr/local/ci-secrets/redhat-appstudio-load-test/aws_region)
export AWS_PROFILE=rhtap-perfscale
export AWS_DEFAULT_OUTPUT=json

NS=tekton-results

cli=oc
clin="$cli -n $NS"

echo "Creating S3 bucket $TEKTON_RESULTS_S3_BUCKET_NAME" >&2
if [ -z "$(aws s3api list-buckets | jq -rc '.Buckets[] | select(.Name =="'"$TEKTON_RESULTS_S3_BUCKET_NAME"'")')" ]; then
    aws s3api create-bucket --bucket "$TEKTON_RESULTS_S3_BUCKET_NAME" --region="$AWS_REGION" --create-bucket-configuration LocationConstraint="$AWS_REGION" | jq -rc
else
    echo "S3 bucket $TEKTON_RESULTS_S3_BUCKET_NAME already exists, skipping creation"
fi

echo "Creating namepsace $NS" >&2
$cli create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -

echo "Creating S3 secret" >&2
$clin create secret generic tekton-results-s3 \
    --from-literal=aws_access_key_id="$(cat /usr/local/ci-secrets/redhat-appstudio-load-test/aws_access_key_id)" \
    --from-literal=aws_secret_access_key="$(cat /usr/local/ci-secrets/redhat-appstudio-load-test/aws_secret_access_key)" \
    --from-literal=aws_region="$AWS_REGION" \
    --from-literal=bucket="$TEKTON_RESULTS_S3_BUCKET_NAME" \
    --from-literal=endpoint="https://s3.$AWS_REGION.amazonaws.com" --dry-run=client -o yaml | $clin apply -f -
