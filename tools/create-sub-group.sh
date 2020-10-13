#!/bin/bash

NAMESPACE="${NAMESPACE:-openshift-updateservice}"

oc create ns $NAMESPACE

cat <<EOF | oc -n $NAMESPACE create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: updateservice-subscription
spec:
  channel: v1
  name: updateservice-operator-package
  installPlanApproval: Automatic
  source: updateservice-catalog
  sourceNamespace: openshift-marketplace
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: updateservice-operatorgroup
spec:
  targetNamespaces:
  - $NAMESPACE
EOF
