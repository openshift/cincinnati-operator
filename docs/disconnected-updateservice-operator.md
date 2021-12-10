# Disconnected UpdateService 

## Make UpdateService Operator available

Follow the official [documentation](https://docs.openshift.com/container-platform/latest/operators/admin/olm-restricted-networks.html) how to make an operator
available to the disconnected cluster.

```
# the process should look like
pm index prune --from-index registry.redhat.io/redhat/redhat-operator-index:v4.8 --packages cincinnati-operator --tag ${DISCONNECTED_REGISTRY}/olm-index/redhat-operator-index:v4.8
podman push ${DISCONNECTED_REGISTRY}/olm-index/redhat-operator-index:v4.8
oc adm catalog mirror ${DISCONNECTED_REGISTRY}/olm-index/redhat-operator-index:v4.8 ${DISCONNECTED_REGISTRY}/olm --registry-config=/run/user/1000/containers/auth.json

cat catalogsource-redhat-operator-index-manifests.yaml
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: my-redhat-operator-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: {DISCONNECTED_REGISTRY}/olm-index/redhat-operator-index:v4.8
  displayName: Offline-pruned-redhat-operators
  publisher: karampok
  updateStrategy:
    registryPoll:
      interval: 30m

cat imageContentSourcePolicy.yaml
---
apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  labels:
    operators.openshift.org/catalog: "true"
  name: redhat-operator-index-0
spec:
  repositoryDigestMirrors:
  - mirrors:
    - {DISCONNECTED_REGISTRY}/olm/openshift-update-service-cincinnati-operator-bundle
      source: registry.redhat.io/openshift-update-service/cincinnati-operator-bundle
```

You can verify that the operator is available by checking

```
oc get packagemanifest cincinnati-operator
> NAME                  CATALOG                           AGE
> cincinnati-operator   Offline-pruned-redhat-operators   31d
```

## Make UpdateService trust the registry certificate

```
oc create configmap trusted-ca -n openshift-config \ 
  --from-file=updateservice-registry=/etc/pki/ca-trust/source/anchors/private-registry.crt
oc patch image.config.openshift.io cluster -p '{"spec":{"additionalTrustedCA":{"name":"trusted-ca"}}}' --type merge
# !updateservice-registry is hardcoded value in the code, cannot be anything else!
```

For details refer the [external registry container CA injection](./external-registry-ca.md).

## Make UpdateService able to login to the registry

```
oc -n openshift-config create secret generic pull-secret \
  --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
  --type=kubernetes.io/dockerconfigjson
```

## Deploy the UpdateService operator

```
oc create ns openshift-update-service
cat <<EOF | oc create -f - 
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cincinnati-subscription
spec:
  channel: v1
  name: cincinnati-operator
  installPlanApproval: Automatic
  source: my-redhat-operator-catalog
  sourceNamespace: openshift-marketplace
---
  apiVersion: operators.coreos.com/v1
  kind: OperatorGroup
  metadata:
    name: cincinnati-operatorgroup
  spec:
    targetNamespaces:
      - openshift-update-service
EOF
```

## Create initial graph data container image 

We need to create the graph data to allow upgrade between version. We can use the latest
data or create our own. For details refer the [init container documentation](./graph-data-init-container.md).

In order to build a container image that contains the graph-data, which will be used as init
container in the updateservice pod: 

```
wget https://github.com/openshift/cincinnati-graph-data/archive/master.tar.gz -O graph.tar.gz
cat <<EOF > Dockerfile
FROM registry.access.redhat.com/ubi8/ubi:8.1
COPY graph.tar.gz /tmp/
RUN mkdir -p /var/lib/cincinnati/graph-data/
CMD exec /bin/bash -c "tar xvzf /tmp/graph.tar.gz -C /var/lib/cincinnati/graph-data/ --strip-components=1"
EOF

podman build -f Dockerfile -t ${DISCONNECTED_REGISTRY}/updateservice/graph-data:v1
podman push ${DISCONNECTED_REGISTRY}/updateservice/graph-data:v1
```
## Mirror the release image and the release content

You might want to review the documentation around disconnected registries to learn about mirroring OpenShift Releases.

```
export UPSTREAM_REGISTRY='quay.io'
export PRODUCT_REPO='openshift-release-dev'
export RELEASE_NAME='ocp-release'
export OCP_RELEASE='4.5.2-x86_64'
export LOCAL_REGISTRY='my-disconnected-registry.example.com:5000'
export LOCAL_SECRET_JSON='/path/to/pull_secret.json'

oc adm -a ${LOCAL_SECRET_JSON} release mirror \
--from=${UPSTREAM_REGISTRY}/${PRODUCT_REPO}/${RELEASE_NAME}:${OCP_RELEASE} \
--to=${LOCAL_REGISTRY}/ocp4 \
--to-release-image=${LOCAL_REGISTRY}/ocp4/release:${OCP_RELEASE}

export OCP_RELEASE='4.5.3-x86_64'

oc adm -a ${LOCAL_SECRET_JSON} release mirror \
--from=${UPSTREAM_REGISTRY}/${PRODUCT_REPO}/${RELEASE_NAME}:${OCP_RELEASE} \
--to=${LOCAL_REGISTRY}/ocp4 \
--to-release-image=${LOCAL_REGISTRY}/ocp4/release:${OCP_RELEASE}
```


## Deploy UpdateService instance

```
cat create-updateservice-instance.yaml
---
apiVersion: updateservice.operator.openshift.io/v1
kind: UpdateService
metadata:
  name: karampok
  namespace: openshift-update-service
spec:
  replicas: 1
  releases: "{DISCONNECTED_REGISTRY}/ocp4/release"
  graphDataImage: "{DISCONNECTED_REGISTRY}/updateservice/graph-data:v4"
```
    
### Known Issues

#### OOMKilled

If you have you mirrored your OpenShift Release Image and Release Content
with an identical path, UpdateService's Graph Builder will take more memory
than allowed by the Deployment configuration and the pod will be OOMKilled.
If this is your case you can copy the release image to a different namespace
in your registry.

```
skopeo copy docker://${DISCONNECTED_REGISTRY}/ocp4:4.5.2-x86_64 docker://${DISCONNECTED_REGISTRY}/release:4.5.2-x86_64 --authfile=/path/to/pull_secret.json
skopeo copy docker://${DISCONNECTED_REGISTRY}/ocp5:4.5.3-x86_64 docker://${DISCONNECTED_REGISTRY}/release:4.5.3-x86_64 --authfile=/path/to/pull_secret.json
```

#### CreateRouteFailed

If you see `ReconcileCompleted` status as `false` with reason `CreateRouteFailed` caused by `host must conform to DNS 1123 naming convention`
and `must be no more than 63 characters`, try creating the Update Service with a shorter name.
    
## Make client cluster use UpdateService instance

Once UpdateService starts, there is a public URL (OCP route) available. This URL is should be configured in the client cluster. 
See [official documentation](https://docs.openshift.com/container-platform/latest/updating/updating-cluster-cli.html#update-changing-update-server-cli_updating-cluster-cli) for details.

```
oc adm upgrade
>  Cluster version is 4.8.22

>  Upgradeable=False
>    Reason: AdminAckRequired
>    Message: Kubernetes 1.22 and therefore OpenShift 4.9 remove several APIs which require admin consideration. Please see the knowledge article https://access.redhat.com/articles/6329921 for details and instructions.
>  warning: Cannot display available updates:
>    Reason: RemoteFailed
>    Message: Unable to retrieve available updates: Get "https://api.openshift.com/api/upgrades_info/v1/graph?arch=amd64&channel=stable-4.8&id=6f878e7c-cb81-41de-a954-bb6766548318&version=4.8.22": dial tcp 18.210.64.188:443: connect: network is unreachable
# ...  old public update service url ...

POLICY_ENGINE_GRAPH_URI="$(oc -n openshift-update-service get updateservice karampok -o jsonpath='{.status.policyEngineURI}/api/upgrades_info/v1/graph')"
PATCH="{\"spec\":{\"upstream\":\"${POLICY_ENGINE_GRAPH_URI}\"}}"
oc patch clusterversion version -p $PATCH --type merge
```


### Make client cluster trust UpdateService server certificate

Unless your UpdateService tls server certificate is somehow trusted by the client OCP cluster (e.g. well known root CA), then you probably get the
following error:

```
oc adm upgrade
> Cluster version is 4.8.22
> Upgradeable=False
>   Reason: AdminAckRequired
>   Message: Kubernetes 1.22 and therefore OpenShift 4.9 remove several APIs which require admin consideration. Please see the knowledge article https://access.redhat.com/articles/6329921 for details and instructions.
> warning: Cannot display available updates:
>   Reason: RemoteFailed
>   Message: Unable to retrieve available updates: Get "https://karampok-policy-engine-route-openshift-update-service.apps.bcn.hub-virtual.lab/api/upgrades_info/v1/graph?arch=amd64&channel=stable-4.8&id=6f878e7c-cb81-41de-a954-bb6766548318&version=4.8.22": 
#  ... x509: certificate signed by unknown authority ....
```

If the client cluster, is different than the cluster where UpdateService was installed, you can either add the CA cert 
on the node or in the proxy [configuration](https://docs.openshift.com/container-platform/latest/networking/enable-cluster-wide-proxy.html#enable-cluster-wide-proxy): 

```
    oc create configmap trusted-updateservice-ca -n openshift-config --from-file=ca-bundle.crt=OCPRootCA.pem
    oc patch proxy/cluster -p '{"spec":{"trustedCA":{"name":"trusted-updateservice-ca"}}}' --type merge
    oc edit proxy/cluster
    # ... wait until pods are restarting ..
```

Finally you should get the following:

```
oc adm upgrade
> Cluster version is 4.8.22
> Upgradeable=False
>  Reason: AdminAckRequired
>  Message: Kubernetes 1.22 and therefore OpenShift 4.9 remove several APIs which require admin consideration. Please see the knowledge article https://access.redhat.com/articles/6329921 for details and instructions.
> Updates:
> VERSION IMAGE
> 4.8.23  {DISCONNECTED_REGISTRY}/ocp4/release@sha256:3fab205d36c66825423274eac90f4c142a18cdf358b4a666a1783d325afba860
```

### Add release signature verification config

To make the release safe, so no extra flags are needed, we can add a release signature configmap:

```
export release=4.8.23
export digest=$(oc adm release info ${DISCONNECTED_REGISTRY}/ocp4/release:${release}-x86_64 --registry-config=${XDG_RUNTIME_DIR}/containers/auth.json -o json | jq  -r .digest)
export signature=$(curl -s "https://mirror.openshift.com/pub/openshift-v4/signatures/openshift/release/${digest//:/=}/signature-1" | base64 -w0 && echo)
cat <<EOF | oc create -f - 
apiVersion: v1
kind: ConfigMap
metadata:
  name: release-image-${release}
  namespace: openshift-config-managed
  labels:
    release.openshift.io/verification-signatures: ""
binaryData:
  ${digest//:/-}: ${signature}
EOF
```

## Update Client Cluster

We can start the update using the CLI and executing:

```
oc adm upgrade --to=<version>
```
    
If this command throws any warnings which are not important for cluster
upgrade, use the argument `--allow-upgrade-with-warnings`. If user wants to
upgrade the cluster to a cluster which is not available in the graph, use
`--force` argument. Note, that cluster upgraded using any of these arguments
may be in unsupportable state.

# Useful tools

## Print the graph

You can print the graph for a specific channel in your UpdateService instance using the commands below

```
sudo dnf install -y graphviz
curl -O https://raw.githubusercontent.com/openshift/cincinnati/master/hack/graph.sh
chmod +x graph.sh
curl -s "https://karampok-policy-engine-route-openshift-update-service.apps.bcn.hub-virtual.lab/api/upgrades_info/v1/graph?channel=fast-4.8"| ./graph.sh | dot -Tpng > graph.png
```

# Useful docs

* https://github.com/openshift/cincinnati/blob/master/docs/user/running-cincinnati.md
* https://github.com/openshift/cincinnati-operator/blob/master/docs/graph-data-init-container.md
* https://github.com/openshift/cincinnati-operator/blob/master/docs/external-registry-ca.md
* Repositories
  * https://github.com/openshift/cincinnati-graph-data/
  * https://github.com/openshift/cincinnati-operator/
  * https://github.com/openshift/cincinnati
