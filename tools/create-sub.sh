#!/bin/bash

NAMESPACE="${NAMESPACE:-updateservice-operator}"

cat <<EOF | oc -n $NAMESPACE create -f - 
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: updateservice-subscription
spec:
  channel: alpha
  name: updateservice-operator-package
  installPlanApproval: Automatic
  source: updateservice-catalog                                                       
  sourceNamespace: openshift-marketplace
EOF
