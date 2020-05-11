# Tools
The Tools directory contains a list of scripts that will assist devlopers, users,
and QE with consuming the cincinnati-operator.

- `token.sh` -> Get a User's token from Quay.io.
- `quay-registry.sh` -> Create an OperatorSource for the cincinnati-operator. An
OperatorSource tells OLM to read from a Quay registry and look operators it can
make available to the cluster.  Set the variable `APP_REGISTRY_NAMESPACE` to a
container registry that has a cincinnati-operator bundle or index image.
- `operator-group.yaml` -> When deploying the cincinnati-operator through OLM,
you'll need to create an OperatorGroup, so that your operator has permission to
land in the `openshift-cincinnati` namespace.
- `subscription.yaml` -> Create this resource to launch the cincinnati-operator.

## Testing with OLM

1. Set `export TOKEN` to either the string returned from `./tools/token.sh` or
an already known TOKEN.
2. `APP_REGISTRY_NAMESPACE=cincinnati ./tools/quay-registry.sh`
3. `oc get operatorsource -n openshift-marketplace | grep $APP_REGISTRY_NAMESPACE`
Expected result:
```bash
$ oc get operatorsource -n openshift-marketplace | grep cincinnati
cincinnati              appregistry   https://quay.io/cnr   cincinnati              cincinnati              Red Hat     Succeeded   The object has been successfully reconciled   17h
```
4. `oc create namespace openshift-cincinnati`
5. `oc create -f ./tools/operator-group.yaml`
6. `oc create -f ./tools/subscription.yaml`
7. Verify everything is working:
```bash
$ oc get installplan -n openshift-cincinnati
oNAME            CSV                          APPROVAL    APPROVED
install-hdjkr   cincinnati-operator.v0.0.1   Automatic   true
$ oc get csv -n openshift-cincinnati
NAME                         DISPLAY               VERSION   REPLACES   PHASE
cincinnati-operator.v0.0.1   Cincinnati Operator   0.0.1                Succeeded
$ oc get pods -n openshift-cincinnati
NAME                                  READY   STATUS    RESTARTS   AGE
cincinnati-operator-79788fb56-8nhsf   1/1     Running   0          33s
```
8. `oc create -f deploy/crds/cincinnati.openshift.io_v1beta1_cincinnati_cr.yaml`
