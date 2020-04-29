# External Registry CA Injection

If you are using a secure external container registry to hold mirrored OpenShift
release images, Cincinnati will need access to this registry in order to build
an upgrade graph.  Here's how you can inject your CA Cert into the Cincinnati
pod.

OpenShift has an external registry API, located at `image.config.openshift.io`,
that we'll use to store the external registry CA Cert.  You can read more about
this API in the [OpenShift documentation](https://docs.openshift.com/container-platform/4.3/registry/configuring-registry-operator.html#images-configuration-cas_configuring-registry-operator).

Create a ConfigMap in the `openshift-config` namespace.  Fill in your CA Cert
and ConfigMap name, but keep the key, `cincinnati-registry`, because it's how
Cincinnati locates your Cert:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: trusted-ca
data:
  cincinnati-registry: |
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
`openshift-config` ConfigMap you created for changes and restart the deployment
if the Cert has changed.
