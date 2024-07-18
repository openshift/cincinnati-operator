#!/bin/bash

### This script is to modify the environment variables in the manifests
### "kustomize edit set configmap" works for the case but operator-sdk errors out on the generated files
### Error generating bundle manifests: related images with valueFrom field unsupported, found in RELATED_IMAGE_OPERAND`

set -ex

# DEFAULT_OPERATOR_IMAGE is a placeholder for cincinnati-operator image placeholder
# During development override this when you want to use an specific image
DEFAULT_OPERATOR_IMAGE="controller:latest"
DEFAULT_OPERAND_IMAGE="quay.io/cincinnati/cincinnati:latest"

RELATED_OPERATOR_IMAGE="${RELATED_IMAGE_OPERATOR:-${DEFAULT_OPERATOR_IMAGE}}"
RELATED_OPERAND_IMAGE="${RELATED_IMAGE_OPERAND:-${DEFAULT_OPERAND_IMAGE}}"

if ! [ -n "$KUBECONFIG" ]; then
	echo "KUBECONFIG environment variable must be set."
	exit 1
fi
if ! [ -n "$GRAPH_DATA_IMAGE" ]; then
	echo "GRAPH_DATA_IMAGE environment variable must be set."
	exit 1
fi

echo "Deploying using ${RELATED_OPERATOR_IMAGE} as operator, ${RELATED_OPERAND_IMAGE} as operand and ${GRAPH_DATA_IMAGE} as graph data image"

SED_CMD="${SED_CMD:-sed}"
${SED_CMD} -i "s|$DEFAULT_OPERAND_IMAGE|$RELATED_OPERAND_IMAGE|" config/manager/manager.yaml
${SED_CMD} -i "s|$DEFAULT_OPERATOR_IMAGE|$RELATED_OPERATOR_IMAGE|" config/manager/manager.yaml
${SED_CMD} -i "s|your-registry/your-repo/your-init-container|$GRAPH_DATA_IMAGE|" config/samples/updateservice.operator.openshift.io_v1_updateservice_cr.yaml
