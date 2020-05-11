package functests

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/cincinnati-operator/pkg/apis"
	"github.com/prometheus/common/log"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestTests(t *testing.T) {
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		log.Errorf("Failed adding apis to scheme, %v", err)
		os.Exit(1)
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tests Suite")
}
