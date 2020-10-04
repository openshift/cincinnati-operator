#!/bin/bash
tag="$1"

[ $# -eq 0 ] && { echo "Usage: $0 <updateservice-graph-data-container image tag>"; exit 1; }

DISCONNECTED_REGISTRY="${DISCONNECTED_REGISTRY:-quay.io/jottofar}"

echo Deploying OSUS using image ${DISCONNECTED_REGISTRY}/updateservice-graph-data-container:${tag}

cat <<EOF | oc -n updateservice-operator create -f -
apiVersion: updateservice.operator.openshift.io/v1
kind: UpdateService
metadata:
  name: disconnected-updateservice
spec:
  replicas: 1
  registry: "quay.io"
  repository: "jottofar/updateservice-graph-data-container"
  graphDataImage: "${DISCONNECTED_REGISTRY}/updateservice-graph-data-container:${tag}"
EOF
