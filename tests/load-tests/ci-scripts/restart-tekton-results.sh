#!/bin/bash

echo "Restarting Tekton Results API" oc rollout restart deployment/tekton-results-api -n tekton-results
oc rollout restart deployment/tekton-results-api -n tekton-results
oc rollout status deployment/tekton-results-api -n tekton-results -w
echo "Restarting Tekton Results Watcher"
oc rollout restart deployment/tekton-results-watcher -n tekton-results
oc rollout status deployment/tekton-results-watcher -n tekton-results -w
