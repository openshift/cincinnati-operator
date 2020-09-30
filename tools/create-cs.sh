 #!/bin/bash
tag="$1"

[ $# -eq 0 ] && { echo "Usage: $0 <cincinnati-operator-registry image tag>"; exit 1; }

DISCONNECTED_REGISTRY="${DISCONNECTED_REGISTRY:-quay.io/jottofar}"

echo Creating CatalogSource for image ${DISCONNECTED_REGISTRY}/cincinnati-operator-registry:${tag} 

cat <<EOF | oc -n openshift-marketplace create -f -
kind: CatalogSource
apiVersion: operators.coreos.com/v1alpha1
metadata:
  name: cincinnati-catalog
spec:
  sourceType: grpc
  displayName: Cincinnati Operator
  publisher: Solutions Engineering
  image: ${DISCONNECTED_REGISTRY}/cincinnati-operator-registry:${tag}
EOF
