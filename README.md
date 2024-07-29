# update-service-operator

This operator is developed using the [operator SDK][operator-sdk], version 1.9.0.
Installation docs are [here][operator-sdk-installation].

## Run locally

To run the operator built from the code using a kubeconfig with `cluster-admin` permissions:

```
export RELATED_IMAGE_OPERAND="quay.io/app-sre/cincinnati:2873c6b"
export OPERATOR_NAME=updateservice-operator
export POD_NAMESPACE=openshift-update-service
### Ensure above namespace exists on the cluster and is the current active
oc create namespace --dry-run=client -o yaml "${POD_NAMESPACE}" | oc apply -f -
oc project "${POD_NAMESPACE}"
KUBECONFIG=path/to/kubeconfig make run
```

## Using an init container to load graph data

The UpdateService graph data is loaded from an init container. Before deploying 
the update-service-operator, you will need to [build and push an init container containing the graph data](docs/graph-data-init-container.md).

## Build operator image

```console
podman build -f ./Dockerfile --platform=linux/amd64 -t your-registry/your-repo/your-update-service-operator:tag
podman push your-registry/your-repo/your-update-service-operator:tag
```

## Deploy operator

```
make deploy
```

By default, operator will be deployed using the default operator image `controller:latest`. If you want to override the default operator image with your image, set 

```
export RELATED_IMAGE_OPERATOR="your-registry/your-repo/your-update-service-opertor-image:tag"
```

## Run functional tests

```
make func-test
```

To run the functional testcases locally, you must set below environment variables as shown below along with optional `RELATED_IMAGE_OPERAND` and `RELATED_IMAGE_OPERATOR`.

```
export KUBECONFIG="path-for-kubeconfig-file"
export GRAPH_DATA_IMAGE="your-registry/your-repo/your-init-container:tag"
```

## Run unit tests

```
make unit-test
```

[operator-sdk]: https://sdk.operatorframework.io/docs/
[operator-sdk-installation]: https://v1-9-x.sdk.operatorframework.io/docs/installation/

## Generating OLM manifests

Here are the steps to generate the operator-framework manifests in the bundle format
* Set the `OPERATOR_VERSION` value in the shell
* Set the `IMG` value pointing to the OSUS operator which should be part of the operator bundle.
* Run `make bundle`.

Example:

```sh
OPERATOR_VERSION=4.9.0
IMG=registry.com/cincinnati-openshift-update-service-operator:v4.6.0
make bundle
```

## Test a PR with a cluster-bot cluster

Follow [ci-docs](https://docs.ci.openshift.org/docs/how-tos/testing-operator-sdk-operators/#launching-clusters-with-operator-built-from-pr-via-cluster-bot). E.g., issuing the following message to "Cluster Bot" in Slack

```
launch 4.16,openshift/cincinnati-operator#185 aws
```


will launch a 4.16 cluster on `aws` and install the built operator from [PR#185](https://github.com/openshift/cincinnati-operator/pull/185).

## Documentation
* [Deploy disconnected update service](./docs/disconnected-updateservice-operator.md)
* [External registry CA injection](./docs/external-registry-ca.md)
* [Using graph data init container](./docs/graph-data-init-container.md)
