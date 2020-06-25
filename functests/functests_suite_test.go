package functests

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/cincinnati-operator/pkg/apis"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
)

func TestTests(t *testing.T) {
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		klog.Errorf("Failed adding apis to scheme, %v", err)
		os.Exit(1)
	}
	if err := routev1.AddToScheme(scheme.Scheme); err != nil {
		klog.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}
	if err := routev1.AddToScheme(scheme.Scheme); err != nil {
		klog.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tests Suite")
}

var _ = AfterSuite(func() {

	err := deleteServiceAccount()
	Expect(err).To(BeNil())

	err = deleteClusterRole()
	Expect(err).To(BeNil())

	err = deleteClusterRoleBinding()
	Expect(err).To(BeNil())

	err = deleteDeployment()
	Expect(err).To(BeNil())

	err = deleteCRD()
	Expect(err).To(BeNil())

	err = deleteNamespace()
	Expect(err).To(BeNil())
})
