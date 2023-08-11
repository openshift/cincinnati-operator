# External Registry CA Injection

**Note:** In a disconnected cluster with cluster-wide CA, Cincinnati will use the pre-configured CA certificates 
to access the external registry. 

If you are using a secure external container registry to hold mirrored OpenShift
release images for which the CA certificates are not added to cluster-wide CA, Cincinnati will need access to this registry in order to build an upgrade graph.  Here's how you can inject your CA Cert into the Cincinnati pod.

OpenShift has an external registry API, located at `image.config.openshift.io`,
that we'll use to store the external registry CA Cert.  You can read more about
this API in the [OpenShift documentation](https://docs.openshift.com/container-platform/4.6/registry/configuring-registry-operator.html#images-configuration-cas_configuring-registry-operator).

Create a ConfigMap in the `openshift-config` namespace.  Fill in your CA Cert
under the key `cincinnati-registry`, because it's how Cincinnati locates your Cert:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: trusted-ca
data:
  updateservice-registry: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
```

Edit the `cluster` resource from the `image.config.openshift.io` API and set
the `additionalTrustedCA` field to the name of the ConfigMap you just created
above.
```bash
$ oc edit image.config.openshift.io cluster
spec:
  additionalTrustedCA:
    name: trusted-ca
```

The Cincinnati Operator will watch the `image.config.openshift.io` API and the
ConfigMap you created in the `openshift-config` namespace for changes, then
restart the deployment if the Cert has changed.
