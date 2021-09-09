module github.com/openshift/cincinnati-operator

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/openshift/api v0.0.0-20210907084939-33af3ff57ff1
	github.com/openshift/cluster-image-registry-operator v0.0.0-20210830135433-48485bb2206c
	github.com/openshift/custom-resource-status v1.1.0
	github.com/openshift/library-go v0.0.0-20210906100234-6754cfd64cb5
	github.com/stretchr/testify v1.7.0
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/klog v1.0.0
	k8s.io/kubectl v0.22.1
	sigs.k8s.io/controller-runtime v0.8.3
)

exclude github.com/openshift/api v3.9.0+incompatible

replace (
	k8s.io/api v0.22.1 => k8s.io/api v0.20.2
	k8s.io/apimachinery v0.22.1 => k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.22.1 => k8s.io/client-go v0.20.2
	k8s.io/kubectl v0.22.1 => k8s.io/kubectl v0.20.2
)
