# cincinnati-operator

## Run locally

To run locally, you must set the operand image as shown below.

```
export OPERAND_IMAGE="quay.io/app-sre/cincinnati:2873c6b" 
operator-sdk run --local
```

## Using an init container to load graph data

The Cincinnati graph data can also be loaded from an init container.
[docs/graph-data-init-container.md](docs/graph-data-init-container.md) 
details how.