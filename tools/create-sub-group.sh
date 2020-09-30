#!/bin/bash

NAMESPACE="${NAMESPACE:-cincinnati-operator}"

oc create ns $NAMESPACE

cat <<EOF | oc -n $NAMESPACE create -f - 
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cincinnati-subscription
spec:
  channel: alpha
  name: cincinnati-operator-package
  installPlanApproval: Automatic
  source: cincinnati-catalog                                                       
  sourceNamespace: openshift-marketplace
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: cincinnati-operatorgroup
spec:
  targetNamespaces:
  - $NAMESPACE
EOF
