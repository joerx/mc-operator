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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	minecraftv1alpha1 "github.com/joerx/minecraft-operator/api/v1alpha1"
)

const serverFinalizer = "minecraft.k8s.learning.yodo.dev/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableServer represents the status of the Deployment reconciliation
	typeAvailableServer = "Available"
	// typeDegradedServer represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedServer = "Degraded"
)

// ServerReconciler reconciles a Server object
type ServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html

//+kubebuilder:rbac:groups=minecraft.k8s.learning.yodo.dev,resources=servers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=minecraft.k8s.learning.yodo.dev,resources=servers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=minecraft.k8s.learning.yodo.dev,resources=servers/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *ServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Server instance
	// The purpose is check if the Custom Resource for the Kind Server
	// is applied on the cluster if not we return nil to stop the reconciliation
	server := &minecraftv1alpha1.Server{}
	err := r.Get(ctx, req.NamespacedName, server)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("server resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get server")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status is available
	if server.Status.Conditions == nil || len(server.Status.Conditions) == 0 {
		meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, server); err != nil {
			log.Error(err, "Failed to update Server status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the server Custom Resource after updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, server); err != nil {
			log.Error(err, "Failed to re-fetch server")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(server, serverFinalizer) {
		log.Info("Adding Finalizer for Server")
		if ok := controllerutil.AddFinalizer(server, serverFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, server); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Server instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isServerMarkedToBeDeleted := server.GetDeletionTimestamp() != nil
	if isServerMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(server, serverFinalizer) {
			log.Info("Performing Finalizer Operations for Server before delete CR")

			// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeDegradedServer,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", server.Name)})

			if err := r.Status().Update(ctx, server); err != nil {
				log.Error(err, "Failed to update Server status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForServer(server)

			// TODO(user): If you add operations to the doFinalizerOperationsForServer method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.
			// Potential operations for Minecraft server: back up the world state to S3 before deleting

			// Re-fetch the server Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, server); err != nil {
				log.Error(err, "Failed to re-fetch server")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeDegradedServer,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", server.Name)})

			if err := r.Status().Update(ctx, server); err != nil {
				log.Error(err, "Failed to update Server status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for Server after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(server, serverFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for Server")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, server); err != nil {
				log.Error(err, "Failed to remove finalizer for Server")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	toCreate := make([]client.Object, 0, 2)

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: server.Name, Namespace: server.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForServer(server)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for Server")

			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", server.Name, err)})

			if err := r.Status().Update(ctx, server); err != nil {
				log.Error(err, "Failed to update Server status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Append here first, create later
		toCreate = append(toCreate, dep)
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	foundSvc := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: server.Name, Namespace: server.Namespace}, foundSvc)
	if err != nil && apierrors.IsNotFound(err) {
		svc, err := r.serviceForServer(server)
		if err != nil {
			log.Error(err, "Failed to define new Service resource for Server")

			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service for the custom resource (%s): (%s)", server.Name, err)})

			if err := r.Status().Update(ctx, server); err != nil {
				log.Error(err, "Failed to update Server status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Append here first, create later
		toCreate = append(toCreate, svc)
	} else if err != nil {
		log.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	// Create all resources in one pass before requeuing
	if len(toCreate) > 0 {
		for _, o := range toCreate {
			log.Info(fmt.Sprintf("Creating a new %s", o.GetObjectKind()), "Namespace", o.GetNamespace(), "Name", o.GetName())
			if err = r.Create(ctx, o); err != nil {
				log.Error(err, fmt.Sprintf("Failed to create new %s", o.GetObjectKind()), "Namespace", o.GetNamespace(), "Name", o.GetName())
				return ctrl.Result{}, err
			}
		}

		// We will requeue the reconciliation so that we can ensure the state and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// We technically don't need the below, we will always be running a single replica per
	// deployment. We still want to ensure nobody accidentally scales it up tho.
	size := getReplicasForServer(server)
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Failed to update Deployment",
				"Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

			// Re-fetch the server Custom Resource
			if err := r.Get(ctx, req.NamespacedName, server); err != nil {
				log.Error(err, "Failed to re-fetch server")
				return ctrl.Result{}, err
			}

			msg := fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", server.Name, err)
			if err = r.setStatusCondition(ctx, server, "Resizing", msg); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the size we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	msg := fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", server.Name, size)
	if err = r.setStatusCondition(ctx, server, "Reconciling", msg); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r ServerReconciler) setStatusCondition(ctx context.Context, server *minecraftv1alpha1.Server, reason, message string) error {
	log := log.FromContext(ctx)

	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer,
		Status: metav1.ConditionTrue, Reason: reason,
		Message: message})

	if err := r.Status().Update(ctx, server); err != nil {
		log.Error(err, "Failed to update Server status")
		return err
	}

	return nil
}

func getReplicasForServer(cr *minecraftv1alpha1.Server) int32 {
	if cr.Spec.Active {
		return 1
	}
	return 0
}

// finalizeServer will perform the required operations before delete the CR.
func (r *ServerReconciler) doFinalizerOperationsForServer(cr *minecraftv1alpha1.Server) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as dependent of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

func (r *ServerReconciler) serviceForServer(server *minecraftv1alpha1.Server) (*corev1.Service, error) {
	ls := labelsForServer(server.Name)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: ls,
			Ports: []corev1.ServicePort{
				{
					Name:       "server",
					Port:       server.Spec.ContainerPort,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString("server"),
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(server, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

// deploymentForServer returns a Server Deployment object
func (r *ServerReconciler) deploymentForServer(server *minecraftv1alpha1.Server) (*appsv1.Deployment, error) {
	ls := labelsForServer(server.Name)
	replicas := int32(1)

	// Get the Operand image
	image, err := imageForServer()
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					// TODO(user): Uncomment the following code to configure the nodeAffinity expression
					// according to the platforms which are supported by your solution. It is considered
					// best practice to support multiple architectures. build your manager image using the
					// makefile target docker-buildx. Also, you can use docker manifest inspect <image>
					// to check what are the platforms supported.
					// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity
					//Affinity: &corev1.Affinity{
					//	NodeAffinity: &corev1.NodeAffinity{
					//		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					//			NodeSelectorTerms: []corev1.NodeSelectorTerm{
					//				{
					//					MatchExpressions: []corev1.NodeSelectorRequirement{
					//						{
					//							Key:      "kubernetes.io/arch",
					//							Operator: "In",
					//							Values:   []string{"amd64", "arm64", "ppc64le", "s390x"},
					//						},
					//						{
					//							Key:      "kubernetes.io/os",
					//							Operator: "In",
					//							Values:   []string{"linux"},
					//						},
					//					},
					//				},
					//			},
					//		},
					//	},
					//},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{false}[0],
						FSGroup:      &[]int64{2000}[0],
						RunAsGroup:   &[]int64{3000}[0],
						RunAsUser:    &[]int64{1000}[0],

						// IMPORTANT: seccomProfile was introduced with Kubernetes 1.19
						// If you are looking for to produce solutions to be supported
						// on lower versions you must remove this option.
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Volumes: []corev1.Volume{{
						Name: "tmp",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}, {
						Name: "datadir",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{}, // TODO: when enabling persistence, use a PVC here
						},
					}, {
						Name: "backupdir",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}},
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "server",
						ImagePullPolicy: corev1.PullIfNotPresent,
						// Ensure restrictive context for the container
						// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{{
							ContainerPort: server.Spec.ContainerPort,
							Name:          "server",
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1280Mi"),
							},
						},
						Env: []corev1.EnvVar{
							{Name: "EULA", Value: fmt.Sprintf("%t", server.Spec.Eula)},
							{Name: "TYPE", Value: server.Spec.ServerType},
							{Name: "VERSION", Value: server.Spec.ServerVersion},
							{Name: "DIFFICULTY", Value: server.Spec.Difficulty},
							{Name: "MAX_PLAYERS", Value: fmt.Sprintf("%d", server.Spec.MaxPlayers)},
							{Name: "MAX_WORLD_SIZE", Value: "10000"},
							{Name: "MAX_BUILD_HEIGHT", Value: "256"},
							{Name: "MAX_TICK_TIME", Value: "60000"},
							{Name: "SPAWN_PROTECTION", Value: "16"},
							{Name: "VIEW_DISTANCE", Value: "10"},
							{Name: "MODE", Value: server.Spec.GameMode},
							{Name: "MOTD", Value: server.Spec.Motd},
							{Name: "LEVEL_TYPE", Value: "DEFAULT"},
							{Name: "LEVEL", Value: "world"},
							{Name: "MODRINTH_ALLOWED_VERSION_TYPE", Value: "release"},
							{Name: "MEMORY", Value: "1024M"}, // memory for the JVM, TODO: make dependent on overall memory allocated to the pod
							{Name: "OVERRIDE_SERVER_PROPERTIES", Value: "false"},
							{Name: "ENABLE_RCON", Value: "false"}, // TODO: enable and do something interesting with it
						},
						VolumeMounts: []corev1.VolumeMount{{
							MountPath: "/tmp",
							Name:      "tmp",
						}, {
							MountPath: "/data",
							Name:      "datadir",
						}, {
							MountPath: "/backups",
							Name:      "backupdir",
						}},
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(server, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

// labelsForServer returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForServer(name string) map[string]string {
	var imageTag string
	image, err := imageForServer()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{"app.kubernetes.io/name": "minecraft-operator",
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/managed-by": "ServerController",
	}
}

// imageForServer gets the Operand image which is managed by this controller
// from the SERVER_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForServer() (string, error) {
	var imageEnvVar = "SERVER_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("Unable to find %s environment variable with the image", imageEnvVar)
	}
	return image, nil
}

func int64Ptr(i int64) *int64 {
	return &i
}

// SetupWithManager sets up the controller with the Manager.
// Note that the Deployment will be also watched in order to ensure its
// desirable state on the cluster
func (r *ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&minecraftv1alpha1.Server{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
