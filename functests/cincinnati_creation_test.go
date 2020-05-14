package functests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	cincinnativ1beta1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("CustomResource", func() {
	RegisterFailHandler(Fail)

	k8sClient, err := getK8sClient()
	Expect(k8sClient).NotTo(BeNil())
	Expect(err).To(BeNil())

	BeforeEach(func() {
		err = waitForDeployment(k8sClient, operatorName)
		Expect(err).To(BeNil())

		labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"name": operatorName}}
		listOptions := metav1.ListOptions{
			LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		}
		pod, err := k8sClient.CoreV1().Pods(operatorNamespace).List(listOptions)
		Expect(err).To(BeNil())
		Expect(string(pod.Items[0].Status.Phase)).Should(Equal("Running"))
		Expect(bool(pod.Items[0].Status.ContainerStatuses[0].Ready)).Should(Equal(true))

		err = waitForService(k8sClient, operatorName+"-metrics")
		Expect(err).To(BeNil())

		err = deployCR()
		Expect(err).To(BeNil())
	})

	AfterEach(func() {
		err = deleteCR()
		Expect(err).To(BeNil())
	})

	When("Testing custom resource", func() {
		It("Should get the custom resource, verify if the deployment is available, pods, services and PodDisruptionBudget are available and route is accessible", func() {
			cincinnatiClient, err := getCincinnatiClient()
			Expect(cincinnatiClient).NotTo(BeNil())
			Expect(err).To(BeNil())

			By("Verifying if Cincinnati custom resource is available")
			result := &cincinnativ1beta1.Cincinnati{}
			err = cincinnatiClient.Get().
				Resource(resource).
				Namespace(operatorNamespace).
				Name(customResourceName).
				Do().
				Into(result)
			Expect(err).To(BeNil())

			By("Verifying if deployment " + customResourceName + " is available")
			err = waitForDeployment(k8sClient, customResourceName)
			Expect(err).To(BeNil())

			By("Verifying if pods are running")
			labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app": customResourceName}}
			listOptions := metav1.ListOptions{
				LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
			}
			pod, err := k8sClient.CoreV1().Pods(operatorNamespace).List(listOptions)
			Expect(string(pod.Items[0].Status.Phase)).Should(Equal("Running"))
			Expect(bool(pod.Items[0].Status.ContainerStatuses[0].Ready)).Should(Equal(true))
			Expect(bool(pod.Items[0].Status.ContainerStatuses[1].Ready)).Should(Equal(true))
			Expect(bool(pod.Items[0].Status.InitContainerStatuses[0].Ready)).Should(Equal(true))
			Expect(err).To(BeNil())

			By("Verifying if " + customResourceName + "-graph-builder service is available")
			err = waitForService(k8sClient, customResourceName+"-graph-builder")
			Expect(err).To(BeNil())

			By("Verifying if " + customResourceName + "-policy-engine service is available")
			err = waitForService(k8sClient, customResourceName+"-policy-engine")
			Expect(err).To(BeNil())

			By("Verifying if PodDisruptionBudget is available")
			// Checks to see if a given PodDisruptionBudget is available after a specified amount of time.
			// If the PodDisruptionBudget is not available after 30 * retries seconds, the condition function returns an error.
			err = wait.Poll(retryInterval, timeout, func() (done bool, err error) {
				_, err2 := k8sClient.PolicyV1beta1().PodDisruptionBudgets(operatorNamespace).Get(customResourceName, metav1.GetOptions{})
				if err2 != nil {
					if apierrors.IsNotFound(err2) {
						klog.Infof("Waiting for availability of %s PodDisruptionBudget\n", operatorName)
						return false, nil
					}
					return false, err2
				}
				return true, nil
			})
			Expect(err).To(BeNil())
			klog.Infof("PodDisruptionBudget %s available\n", operatorName)

			By("Verifying if route is available")
			crClient, err := getCrClient()
			Expect(crClient).NotTo(BeNil())
			Expect(err).To(BeNil())

			route := &routev1.Route{}
			err = crClient.Get(context.Background(), client.ObjectKey{
				Namespace: operatorNamespace,
				Name:      routeName,
			}, route)
			Expect(err).To(BeNil())

			By("Verifying if route is accessible")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			httpClient := &http.Client{Transport: tr}

			req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/api/upgrades_info/v1/graph?channel=stable-4.4", route.Spec.Host), nil)
			Expect(err).To(BeNil())
			req.Header.Set("Accept", "application/json")
			err = wait.Poll(retryInterval, timeout, func() (done bool, err error) {
				resp, err2 := httpClient.Do(req)
				Expect(err2).To(BeNil())
				if resp.StatusCode > 200 {
					klog.Infof("Waiting for availability of %s route\n", routeName)
					return false, nil
				}
				klog.Infof("Route %s available\n", routeName)
				return true, nil
			})
			Expect(err).To(BeNil())
		})

	})
})
