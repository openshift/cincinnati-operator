 #!/bin/bash
tag="$1"

[ $# -eq 0 ] && { echo "Usage: $0 <updateservice-operator-registry image tag>"; exit 1; }

DISCONNECTED_REGISTRY="${DISCONNECTED_REGISTRY:-quay.io/jottofar}"

echo Creating CatalogSource for image ${DISCONNECTED_REGISTRY}/updateservice-operator-registry:${tag} 

cat <<EOF | oc -n openshift-marketplace create -f -
kind: CatalogSource
apiVersion: operators.coreos.com/v1alpha1
metadata:
  name: updateservice-catalog
spec:
  sourceType: grpc
  displayName: OpenShift Update Service
  publisher: Solutions Engineering
  image: ${DISCONNECTED_REGISTRY}/updateservice-operator-registry:${tag}
EOF
