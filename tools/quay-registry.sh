#!/bin/bash

set -e

PROJECT_ROOT="$(readlink -e $(dirname "$BASH_SOURCE[0]")/../)"

MARKETPLACE_NAMESPACE="${MARKETPLACE_NAMESPACE:-openshift-marketplace}"
APP_REGISTRY_NAMESPACE="${APP_REGISTRY_NAMESPACE:-cincinnati}"

if [ -z "${TOKEN}" ]; then
    if [ -z "${QUAY_USERNAME}" ]; then
	echo "QUAY_USERNAME"
	read QUAY_USERNAME
    fi

    if [ -z "${QUAY_PASSWORD}" ]; then
	echo "QUAY_PASSWORD"
	read -s QUAY_PASSWORD
    fi

    TOKEN=$("${PROJECT_ROOT}"/tools/token.sh $QUAY_USERNAME $QUAY_PASSWORD)
fi

set +e

cat <<EOF | oc create -f -
apiVersion: v1
kind: Secret
metadata:
  name: quay-registry-$APP_REGISTRY_NAMESPACE
  namespace: "${MARKETPLACE_NAMESPACE}"
type: Opaque
stringData:
      token: "$TOKEN"
EOF

cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1
kind: OperatorSource
metadata:
  name: "${APP_REGISTRY_NAMESPACE}"
  namespace: "${MARKETPLACE_NAMESPACE}"
spec:
  type: appregistry
  endpoint: https://quay.io/cnr
  registryNamespace: "${APP_REGISTRY_NAMESPACE}"
  displayName: "${APP_REGISTRY_NAMESPACE}"
  publisher: "Red Hat"
  authorizationToken:
    secretName: "quay-registry-${APP_REGISTRY_NAMESPACE}"
EOF
