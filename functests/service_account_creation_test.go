package functests

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Service Account", func() {
	serviceAccount := &k8sv1.ServiceAccount{
		TypeMeta:   metav1.TypeMeta{Kind: "ServiceAccount", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: operatorName},
	}
	Context("Testing service account", func() {
		It("Create and delete service account", func() {
			By("Creating Service Account")

			k8sClient, err := GetK8sClient()
			Expect(err).To(BeNil())
			Expect(k8sClient).NotTo(BeNil())

			_, err2 := k8sClient.CoreV1().ServiceAccounts("default").Create(serviceAccount)
			Expect(err2).To(BeNil())

			By("Deleting Service Account")
			err = k8sClient.CoreV1().ServiceAccounts("default").Delete(serviceAccount.Name, &metav1.DeleteOptions{})
			Expect(err).To(BeNil())
		})
	})
})
