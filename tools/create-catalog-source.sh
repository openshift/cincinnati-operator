#!/bin/bash
tag="$1"

if test 1 -ne $#
then
  echo "Usage: $0 OPERATOR_IMAGE_TAG" >&2
  exit 1
fi

REGISTRY="${REGISTRY:-quay.io/updateservice}"

echo "Creating CatalogSource for image ${REGISTRY}/updateservice-operator-registry:${tag}"

cat <<EOF | oc -n openshift-marketplace create -f -
kind: CatalogSource
apiVersion: operators.coreos.com/v1alpha1
metadata:
  name: updateservice-catalog
spec:
  sourceType: grpc
  displayName: OpenShift Update Service
  publisher: Solutions Engineering
  image: ${REGISTRY}/updateservice-operator-registry:${tag}
EOF
