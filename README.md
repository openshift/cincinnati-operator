# cincinnati-operator

## Run locally

To run locally, you must set the operand image as shown below.

```
export OPERAND_IMAGE="quay.io/app-sre/cincinnati:2873c6b" 
operator-sdk run --local
```

## Run latest

```
oc apply -k deploy
```

## Deploy using Operator Lifecycle Manager (OLM)

You will first need to get an access token from quay.io and use it to create a
Secret:

```
TOKEN=$(curl -sH "Content-Type: application/json" -XPOST https://quay.io/cnr/api/v1/users/login -d '
{
    "user": {
        "username": "'"${QUAY_USERNAME}"'",
        "password": "'"${QUAY_PASSWORD}"'"
    }
}' | jq -r '.token')

oc create secret generic quay-registry-cincinnati -n openshift-marketplace --from-literal="token=$TOKEN"
```

Then make cincinnati-operator visible to OLM/OperatorHub:

```
oc apply -k deploy/olm/marketplace
```

**Optional** To instantiate the operator, you will need to create a Subscription:

```
oc apply -k deploy/olm/subscription
```

## Using an init container to load graph data

The Cincinnati graph data is loaded from an init container. Before deploying 
the cincinnati-operator, you will need to [build and push an init container containing the graph data](docs/graph-data-init-container.md).
