/*
Copyright 2024 joerx.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	//nolint:golint
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	minecraftv1alpha1 "github.com/joerx/minecraft-operator/api/v1alpha1"
)

var _ = Describe("Server controller", func() {
	Context("Server controller test", func() {

		const ServerName = "test-server"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ServerName,
				Namespace: ServerName,
			},
		}

		typeNamespaceName := types.NamespacedName{
			Name:      ServerName,
			Namespace: ServerName,
		}
		server := &minecraftv1alpha1.Server{}

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Create(ctx, namespace)
			Expect(err).To(Not(HaveOccurred()))

			By("Setting the Image ENV VAR which stores the Operand image")
			err = os.Setenv("SERVER_IMAGE", "example.com/image:test")
			Expect(err).To(Not(HaveOccurred()))

			By("creating the custom resource for the Kind Server")
			err = k8sClient.Get(ctx, typeNamespaceName, server)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				server := &minecraftv1alpha1.Server{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ServerName,
						Namespace: namespace.Name,
					},
					Spec: minecraftv1alpha1.ServerSpec{
						ContainerPort: 25565,
					},
				}

				err = k8sClient.Create(ctx, server)
				Expect(err).To(Not(HaveOccurred()))
			}
		})

		AfterEach(func() {
			By("removing the custom resource for the Kind Server")
			found := &minecraftv1alpha1.Server{}
			err := k8sClient.Get(ctx, typeNamespaceName, found)
			Expect(err).To(Not(HaveOccurred()))

			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), found)
			}, 2*time.Minute, time.Second).Should(Succeed())

			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations.
			// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, namespace)

			By("Removing the Image ENV VAR which stores the Operand image")
			_ = os.Unsetenv("SERVER_IMAGE")
		})

		It("should successfully reconcile a custom resource for Server", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &minecraftv1alpha1.Server{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the custom resource created")
			serverReconciler := &ServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := serverReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func() error {
				found := &appsv1.Deployment{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition added to the Server instance")
			Eventually(func() error {
				if server.Status.Conditions != nil &&
					len(server.Status.Conditions) != 0 {
					latestStatusCondition := server.Status.Conditions[len(server.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{
						Type:   typeAvailableServer,
						Status: metav1.ConditionTrue,
						Reason: "Reconciling",
						Message: fmt.Sprintf(
							"Deployment for custom resource (%s) created successfully",
							server.Name),
					}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the Server instance is not as expected")
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
