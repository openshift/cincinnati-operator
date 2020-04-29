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