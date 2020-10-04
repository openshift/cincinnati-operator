#!/bin/bash

NAMESPACE="${NAMESPACE:-updateservice-operator}"

cat <<EOF | oc -n $NAMESPACE create -f - 
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: updateservice-operatorgroup
spec:
  targetNamespaces:
  - $NAMESPACE
EOF
