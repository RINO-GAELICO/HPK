#!/bin/bash

####### Preamble ###############
# Ensure Testing Namespace
if [[ -z "${TEST_NAMESPACE}" ]]; then
  # Define namespace based on the current directory's name
  export TEST_NAMESPACE=${PWD##*/}
fi
################################


# Remove pod
kubectl delete -f manifest.yaml -n "${TEST_NAMESPACE}"

# Remove namespace
kubectl delete namespace "${TEST_NAMESPACE}" &

sleep 5

kubectl get namespace "${TEST_NAMESPACE}" -o json \
  | tr -d "\n" | sed "s/\"finalizers\": \[[^]]\+\]/\"finalizers\": []/" \
  | kubectl replace --raw /api/v1/namespaces/${TEST_NAMESPACE}/finalize -f -
