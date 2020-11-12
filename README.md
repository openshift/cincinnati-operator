# OpenShift Update Service Operator

This operator is developed using the [operator SDK][operator-sdk], version 0.19.
Installation docs are [here][operator-sdk-installation].

## Run locally

To run locally, you must set the operand image as shown below.

```sh
export OPERAND_IMAGE="quay.io/app-sre/cincinnati:2873c6b" 
export IMG="example.com/you/update-service-operator:latest" # somewhere you can push
make docker-build
make docker-push
make deploy
```

## Using an init container to load graph data

The graph data is loaded from an init container.
Before deploying the Update Service operator, you will need to [build and push an init container containing the graph data](docs/graph-data-init-container.md).

## Deploy operator

```
make deploy
```

By default, operator will be deployed using the default operator image `controller:latest`. If you want to override the default operator image with your image, set 

```
export IMG="your-registry/your-repo/your-update-service-opertor-image:tag"
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
[operator-sdk-installation]: https://v0-19-x.sdk.operatorframework.io/docs/install-operator-sdk/
