# Graph Data Init Container

Normally, the graph data is fetched directly from the Cincinnati graph
data repository at: http://github.com/openshift/cincinnati-graph-data.
In environments where an internet connection is not available, loading
from an init container is another way to make the graph data available
to Cincinnati.

The role of the init container is to provide a copy of the graph data.
During pod initialization, the init container copies the data to a volume
at /var/cincinnati/graph-data. The graph builder is configured to read 
the graph data from the same location.

## Build the graph data init container

An example of how to build an init container can be found in ./dev/Dockerfile.
In the example, the image clones the Cincinnati graph data repository,
http://github.com/openshift/cincinnati-graph-data.

````
podman build -f ./dev/Dockerfile -t quay.io/rwsu/cincinnati-graph-data-
container:latest
podman push quay.io/rwsu/cincinnati-graph-data-container:latest
````

## Configure the operator to use the init container

Edit the Cincinnati CR to include a new parameter graphDataImage.
The value should be set to the location of your init container image.

For the example above:
```
apiVersion: cincinnati.openshift.io/v1alpha1
kind: Cincinnati
metadata:
  name: example-cincinnati
spec:
  replicas: 1
  registry: "quay.io"
  repository: "openshift-release-dev/ocp-release"
  graphDataImage: "quay.io/rwsu/cincinnati-graph-data-container:latest"
```

The gitHubOrg, gitHubRepo, and branch parameters in the CR are used
to configure the graph builder to fetch the graph data from a
git repository. If those parameters and graphDataImage are both specified,
graphDataImage will take precedence. The init container will be used in
lieu of fetching the data from git.