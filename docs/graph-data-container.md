# Graph Data Container

Normally, the graph data is fetched directly from the Cincinnati graph
data repository at: [https://github.com/openshift/cincinnati-graph-data](https://github.com/openshift/cincinnati-graph-data).
In environments where an internet connection is not available, loading
from a graph data container is another way to make the graph data available
to Cincinnati.

The contents of the graph data container is refreshed each time a
graph update is triggered.  Thus it is possible to update the graph
data dynamically by tagging and pushing a new image to the repository.
When Cincinnati next refreshes it will include this updated graph data
automatically.

## Build the graph data container

An example Dockerfile of how to build a graph data container can be found in ./dev/Dockerfile.
In the example, the image takes a tarball of the Cincinnati graph data repository.
Cincinnati unpacks the graph data to /var/lib/cincinnati/graph-data/dkrv2-secondary-metadata-scrape2.

Build and push the image to your own repository. 

````
podman build -f ./dev/Dockerfile -t quay.io/rwsu/cincinnati-graph-data-container:latest
podman push quay.io/rwsu/cincinnati-graph-data-container:latest
````
Depending upon your setup you need to make the repository public or private to make sure the operator can fetch the image from it.

## Configure the operator to use the graph data container

Edit the Cincinnati CR to include a new parameter graphDataImage.
The value should be set to the location where you pushed your graph data
container image.

For the example above:
```
apiVersion: cincinnati.openshift.io/v1beta1
kind: Cincinnati
metadata:
  name: example-name
  namespace: example-namespace
spec:
  replicas: 1
  releases: quay.io/openshift-release-dev/ocp-release
  graphDataImage: quay.io/rwsu/cincinnati-graph-data-container:latest
```
