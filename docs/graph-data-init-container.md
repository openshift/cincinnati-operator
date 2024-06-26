# Graph Data Init Container

Normally, the graph data is fetched directly from the Cincinnati graph
data repository at: [https://github.com/openshift/cincinnati-graph-data](https://github.com/openshift/cincinnati-graph-data).
In environments where an internet connection is not available, loading
from an init container is another way to make the graph data available
to Cincinnati.

The role of the init container is to provide a copy of the graph data.
During pod initialization, the init container copies the data to a volume
at /var/lib/cincinnati/graph-data. The graph builder is configured to read 
the graph data from the same location.

## Build the graph data init container

An example Dockerfile of how to build an init container can be found in ./dev/Dockerfile.
In the example, the image takes a tarball of the Cincinnati graph data repository.
When the init container runs, it untars the data to /var/lib/cincinnati/graph-data.

Build and push the image to your own repository. 

````
podman build -f ./dev/Dockerfile --platform=linux/amd64 -t your-registry/your-repo/your-cincinnati-graph-data-container:tag
podman push your-registry/your-repo/your-cincinnati-graph-data-container:tag
````
Depending upon your setup you need to make the repository public or private to make sure the operator can fetch the image from it.

## Configure the operator to use the init container

Edit the Cincinnati CR to include a new parameter graphDataImage.
The value should be set to the location where you pushed your init 
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
  graphDataImage: your-registry/your-repo/your-cincinnati-graph-data-container:tag
```
