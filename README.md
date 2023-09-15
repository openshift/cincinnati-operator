# update-service-operator

This operator is developed using the [operator SDK][operator-sdk], version 1.9.0.
Installation docs are [here][operator-sdk-installation].

## Run locally

To run locally, you must set the operand image as shown below.

```
export RELATED_IMAGE_OPERAND="quay.io/app-sre/cincinnati:2873c6b"
operator-sdk run --local
```

## Using a graph data container

The UpdateService graph data is loaded from a graph data container. Before deploying
the update-service-operator, you will need to [build and push a container containing the graph data](docs/graph-data-container.md).

## Deploy operator

```
make deploy
```

By default, operator will be deployed using the default operator image `quay.io/updateservice/update-service-operator:latest`. If you want to override the default operator image with your image, set 

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
export GRAPH_DATA_IMAGE="your-registry/your-repo/your-graph-data-container:tag"
```

## Run unit tests

```
make unit-test
```

[operator-sdk]: https://sdk.operatorframework.io/docs/
[operator-sdk-installation]: https://v1-9-x.sdk.operatorframework.io/docs/installation/

## Generating OLM manifests

Here are the steps to generate the operator-framework manifests in the bundle format
* Set the `VERSION` value in the shell
* Set the `IMG` value pointing to the OSUS operator which should be part of the operator bundle.
* Run `make bundle`.

Example:

```sh
VERSION=4.9.0
IMG=registry.com/cincinnati-openshift-update-service-operator:v4.6.0
make bundle
```

## Documentation
* [Deploy disconnected update service](./docs/disconnected-updateservice-operator.md)
* [External registry CA injection](./docs/external-registry-ca.md)
* [Using graph data container](./docs/graph-data-container.md)
