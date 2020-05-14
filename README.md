# cincinnati-operator

## Run locally

To run locally, you must set the operand image as shown below.

```
export OPERAND_IMAGE="quay.io/app-sre/cincinnati:2873c6b" 
operator-sdk run --local
```

## Using an init container to load graph data

The Cincinnati graph data is loaded from an init container. Before deploying 
the cincinnati-operator, you will need to [build and push an init container containing the graph data](docs/graph-data-init-container.md).

## Deploy operator

```
make deploy
```

By default, operator will be deployed using the default operator image `quay.io/cincinnati/cincinnati-operator:latest`. If you want to override the default operator image with your image, set 

```
export OPERATOR_IMAGE="your-registry/your-repo/your-cincinnati-opertor-image:tag"
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
