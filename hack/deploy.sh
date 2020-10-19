#!/bin/bash

set -e


DEFAULT_OPERATOR_IMAGE="quay.io/cincinnati/cincinnati-operator:latest"
DEFAULT_OPERAND_IMAGE="quay.io/app-sre/cincinnati:2873c6b"

OPERATOR_IMAGE="${OPERATOR_IMAGE:-${DEFAULT_OPERATOR_IMAGE}}"
OPERAND_IMAGE="${OPERAND_IMAGE:-${DEFAULT_OPERAND_IMAGE}}"

if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
	OPERATOR_IMAGE="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:cincinnati-operator"
	GRAPH_DATA_IMAGE="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:cincinnati-graph-data-container"

	echo "Openshift CI detected, deploying using image $OPERATOR_IMAGE and ${GRAPH_DATA_IMAGE}"

else
	if ! [ -n "$KUBECONFIG" ]; then
		echo "KUBECONFIG environment variable must be set."
		exit 1
	fi
	if ! [ -n "$GRAPH_DATA_IMAGE" ]; then
		echo "GRAPH_DATA_IMAGE environment variable must be set."
		exit 1
	fi
fi

sed -i "s|quay.io/cincinnati/cincinnati:latest|$OPERAND_IMAGE|" deploy/operator.yaml
sed -i "s|$DEFAULT_OPERATOR_IMAGE|$OPERATOR_IMAGE|" deploy/operator.yaml
sed -i "s|your-registry/your-repo/your-init-container|$GRAPH_DATA_IMAGE|" deploy/crds/cincinnati.openshift.io_v1beta1_cincinnati_cr.yaml

NAMESPACE="openshift-cincinnati"
oc create namespace $NAMESPACE

oc apply -f deploy/service_account.yaml -n $NAMESPACE
oc apply -f deploy/role.yaml -n $NAMESPACE
oc apply -f deploy/role_binding.yaml -n $NAMESPACE
oc apply -f deploy/operator.yaml -n $NAMESPACE
oc apply -f deploy/crds/cincinnati.openshift.io_cincinnatis_crd.yaml -n $NAMESPACE
