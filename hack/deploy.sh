#!/bin/bash

set -e

DEFAULT_OPERATOR_IMAGE="quay.io/updateservice/updateservice-operator:latest"
DEFAULT_OPERAND_IMAGE="quay.io/app-sre/updateservice:2873c6b"

OPERATOR_IMAGE="${OPERATOR_IMAGE:-${DEFAULT_OPERATOR_IMAGE}}"
OPERAND_IMAGE="${OPERAND_IMAGE:-${DEFAULT_OPERAND_IMAGE}}"

if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
	OPERATOR_IMAGE="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:updateservice-operator"
	GRAPH_DATA_IMAGE="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:updateservice-graph-data-container"

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

sed -i "s|quay.io/updateservice/updateservice:latest|$OPERAND_IMAGE|" deploy/operator.yaml
sed -i "s|$DEFAULT_OPERATOR_IMAGE|$OPERATOR_IMAGE|" deploy/operator.yaml
sed -i "s|your-registry/your-repo/your-init-container|$GRAPH_DATA_IMAGE|" deploy/crds/updateservice.operator.openshift.io_v1_updateservice_cr.yaml

NAMESPACE="openshift-updateservice"
oc create namespace $NAMESPACE

oc apply -f deploy/service_account.yaml -n $NAMESPACE
oc apply -f deploy/role.yaml -n $NAMESPACE
oc apply -f deploy/role_binding.yaml -n $NAMESPACE
oc apply -f deploy/operator.yaml -n $NAMESPACE
oc apply -f deploy/crds/updateservice.operator.openshift.io_updateservices_crd.yaml -n $NAMESPACE
