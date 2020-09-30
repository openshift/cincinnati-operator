#!/bin/bash
tag="$1"

[ $# -eq 0 ] && { echo "Usage: $0 <cincinnati-graph-data-container image tag>"; exit 1; }

DISCONNECTED_REGISTRY="${DISCONNECTED_REGISTRY:-quay.io/jottofar}"

echo Deploying OSUS using image ${DISCONNECTED_REGISTRY}/cincinnati-graph-data-container:${tag}

cat <<EOF | oc -n cincinnati-operator create -f -
apiVersion: cincinnati.openshift.io/v1beta1
kind: Cincinnati
metadata:
  name: disconnected-cincinnati
spec:
  replicas: 1
  registry: "quay.io"
  repository: "jottofar/cincinnati-graph-data-container"
  graphDataImage: "${DISCONNECTED_REGISTRY}/cincinnati-graph-data-container:${tag}"
EOF
