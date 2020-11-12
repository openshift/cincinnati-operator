# updateservice-operator

This operator is developed using the [operator SDK][operator-sdk], version 1.0.0.
Installation docs are [here][operator-sdk-installation].

## Run locally

To run locally, you must set the operand image as shown below.

```
export OPERAND_IMAGE="quay.io/app-sre/updateservice:2873c6b" 
operator-sdk run --local
```

## Using an init container to load graph data

The UpdateService graph data is loaded from an init container. Before deploying 
the updateservice-operator, you will need to [build and push an init container containing the graph data](docs/graph-data-init-container.md).

## Deploy operator

```
make deploy
```

By default, operator will be deployed using the default operator image `quay.io/updateservice/updateservice-operator:latest`. If you want to override the default operator image with your image, set 

```
export OPERATOR_IMAGE="your-registry/your-repo/your-updateservice-opertor-image:tag"
```

## Run functional tests

```
make func-test
```

To run the functional testcases locally, you must set below environment variables as shown below along with optional `OPERAND_IMAGE` and `OPERATOR_IMAGE`.

```
export KUBECONFIG="path-for-kubeconfig-file"
export GRAPH_DATA_IMAGE="your-registry/your-repo/your-init-container:tag"
```

## Run unit tests

```
make unit-test
```

[operator-sdk]: https://sdk.operatorframework.io/docs/
[operator-sdk-installation]: https://v1-0-x.sdk.operatorframework.io/docs/installation/install-operator-sdk/
