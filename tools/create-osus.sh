#!/bin/bash
tag="$1"

if test 1 -ne $#
then
  echo "Usage: $0 GRAPH_DATA_CONTAINER_IMAGE_TAG" >&2
  exit 1
fi

REGISTRY="${REGISTRY:-quay.io/updateservice}"

echo Deploying OSUS using image ${REGISTRY}/cincinnati-graph-data-container:${tag}

cat <<EOF | oc -n openshift-updateservice create -f -
apiVersion: updateservice.operator.openshift.io/v1
kind: UpdateService
metadata:
  name: example
spec:
  replicas: 1
  registry: "quay.io"
  repository: "updateservice/cincinnati-graph-data-container"
  graphDataImage: "${REGISTRY}/cincinnati-graph-data-container:${tag}"
EOF
