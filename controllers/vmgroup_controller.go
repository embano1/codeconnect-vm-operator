/*


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

package controllers

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"golang.org/x/sync/errgroup"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "codeconnect/operator/api/v1alpha1"
)

const (
	finalizerID       = "vm-operator"
	defaultNameLength = 8 // length of generated names
	defaultRequeue    = 20 * time.Second
	successMessage    = "successfully reconciled VmGroup"
)

var (
	letters = []rune("abcdefghijklmnopqrstuvwxyz")
)

// VmGroupReconciler reconciles a VmGroup object
type VmGroupReconciler struct {
	client.Client
	Finder       *find.Finder
	ResourcePool *object.ResourcePool
	VC           *govmomi.Client // owns vCenter connection
	Log          logr.Logger
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=vm.codeconnect.vmworld.com,resources=vmgroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vm.codeconnect.vmworld.com,resources=vmgroups/status,verbs=get;update;patch

func (r *VmGroupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("vmgroup", req.NamespacedName)

	vg := &vmv1alpha1.VmGroup{}
	if err := r.Client.Get(ctx, req.NamespacedName, vg); err != nil {
		// add some debug information if it's not a NotFound error
		if !k8serr.IsNotFound(err) {
			log.Error(err, "unable to fetch VmGroup")
		}
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	msg := fmt.Sprintf("received reconcile request for %q (namespace: %q)", vg.GetName(), vg.GetNamespace())
	log.Info(msg)

	// is object marked for deletion?
	if !vg.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("VmGroup marked for deletion")
		// The object is being deleted
		if containsString(vg.ObjectMeta.Finalizers, finalizerID) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(ctx, r.Finder, vg); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			vg.ObjectMeta.Finalizers = removeString(vg.ObjectMeta.Finalizers, finalizerID)
			if err := r.Update(ctx, vg); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "could not remove finalizer")
			}
		}
		// finalizer already removed, nothing to do
		return ctrl.Result{}, nil
	}

	// register our finalizer if it does not exist
	if !containsString(vg.ObjectMeta.Finalizers, finalizerID) {
		vg.ObjectMeta.Finalizers = append(vg.ObjectMeta.Finalizers, finalizerID)
		if err := r.Update(ctx, vg); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "could not add finalizer")
		}
	}

	// govmomi error type used for casting
	var nfe *find.NotFoundError
	desired := vg.Spec.Replicas

	// check if VmGroup folder exists
	_, err := getVMGroup(ctx, r.Finder, getGroupName(vg.Namespace, vg.Name))
	exists := true
	if err != nil {
		// standard type cast does not work since it's a wrapped error
		if errors.As(err, &nfe) {
			log.Info("VmGroup folder does not exist, creating folder")
			exists = false
		} else {
			// TODO: go fancy with error handling to decide whether error is permanent or temporary
			msg := "could not get VmGroup from vCenter"
			log.Error(err, msg)

			vg.Status = createStatus(vmv1alpha1.ErrorStatusPhase, msg, err, nil, desired)

			// ignoring this VmGroup in the future due to unknown error
			return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}
	}

	// create VmGroup folder
	if !exists {
		log.Info("creating VmGroup in vCenter")
		_, err = createVMGroup(ctx, r.Finder, getGroupName(vg.Namespace, vg.Name))
		if err != nil {
			// TODO: go fancy with error handling to decide whether error is permanent or temporary
			msg := "could not create VmGroup in vCenter"
			log.Error(err, msg)

			vg.Status = createStatus(vmv1alpha1.ErrorStatusPhase, msg, err, nil, desired)

			// ignoring this VmGroup in the future due to unknown error
			return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}
		exists = true
	}

	// get replicas (VMs) for VmGroup
	vms, err := getReplicas(ctx, r.Finder, getGroupName(vg.Namespace, vg.Name))
	if err != nil {
		if errors.As(err, &nfe) {
			exists = false
		} else {
			// TODO: go fancy with error handling to decide whether error is permanent or temporary
			msg := "could not get replicas for VmGroup from vCenter"
			log.Error(err, msg)

			vg.Status = createStatus(vmv1alpha1.ErrorStatusPhase, msg, err, nil, desired)

			// ignoring this VmGroup in the future due to unknown error
			return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}
	}

	eg, egCtx := errgroup.WithContext(ctx) // used for concurrent operations against vCenter

	// create replicas (VMs)
	if !exists {
		msg := fmt.Sprintf("no VMs found for VmGroup, creating %d replica(s)", desired)
		log.Info(msg)

		lim := newLimiter(defaultConcurrency)

		// TODO: process async and return early
		for i := 0; i < int(desired); i++ {
			lim.acquire()
			vmName := fmt.Sprintf("%s-replica-%s", vg.Name, generateName())
			msg := fmt.Sprintf("creating clone %q from template %q", vmName, vg.Spec.Template)
			log.Info(msg)

			groupPath := vmPath + "/" + getGroupName(vg.Namespace, vg.Name)

			eg.Go(func() error {
				defer lim.release()
				return cloneVM(egCtx, r.Finder, vg.Spec.Template, vmName, groupPath, r.ResourcePool, vg.Spec)
			})
		}

		err = eg.Wait()
		if err != nil {
			msg := "could not create replica(s)"
			log.Error(err, msg)

			if errors.As(err, &nfe) {
				vg.Status = createStatus(vmv1alpha1.ErrorStatusPhase, msg, err, nil, desired)
				// ignoring in the future due to permanent error
				return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
			}

			// TODO: be smarter about how we calculate "current" count
			vg.Status = createStatus(vmv1alpha1.PendingStatusPhase, msg, err, nil, desired)
			// retry after some time
			return ctrl.Result{RequeueAfter: defaultRequeue}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}

		status := createStatus(vmv1alpha1.RunningStatusPhase, successMessage, nil, &desired, desired)
		vg.Status = status

		// we're done, return successfully
		return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
	}

	// reaching here means (some) replicas exist, checking for diffs
	current := int32(len(vms))
	lim := newLimiter(defaultConcurrency)

	switch {
	case current < desired:
		diff := desired - current
		msg := fmt.Sprintf("too few replicas, creating %d replica(s)", diff)
		log.Info(msg)

		for i := 0; i < int(diff); i++ {
			lim.acquire()

			vmName := fmt.Sprintf("%s-replica-%s", vg.Name, generateName())
			msg := fmt.Sprintf("creating virtual machine %q", vmName)
			log.Info(msg)

			groupPath := vmPath + "/" + getGroupName(vg.Namespace, vg.Name)

			eg.Go(func() error {
				defer lim.release()
				return cloneVM(egCtx, r.Finder, vg.Spec.Template, vmName, groupPath, r.ResourcePool, vg.Spec)
			})
		}

		err = eg.Wait()
		if err != nil {
			msg := "could not create replica(s)"
			log.Error(err, msg)

			if errors.As(err, &nfe) {
				vg.Status = createStatus(vmv1alpha1.ErrorStatusPhase, msg, err, &current, desired)

				// ignoring in the future due to permanent error
				return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
			}

			status := createStatus(vmv1alpha1.PendingStatusPhase, msg, err, &current, desired)
			vg.Status = status

			return ctrl.Result{RequeueAfter: defaultRequeue}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}

	case current > desired:
		diff := current - desired
		msg := fmt.Sprintf("too many replicas, deleting %d", diff)
		log.Info(msg)

		for i := 0; i < int(diff); i++ {
			lim.acquire()

			vm := vms[i]
			msg := fmt.Sprintf("deleting virtual machine %q", vm.Name())
			log.Info(msg)

			eg.Go(func() error {
				defer lim.release()
				return deleteVM(egCtx, vm)
			})
		}

		err = eg.Wait()
		if err != nil {
			msg := "could not delete replica(s)"
			log.Error(err, msg)

			status := createStatus(vmv1alpha1.PendingStatusPhase, msg, err, &current, desired)
			status.CurrentReplicas = &current
			vg.Status = status

			return ctrl.Result{RequeueAfter: defaultRequeue}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}

	default:
		log.Info("replica count in sync, checking power state")
		for i := 0; i < len(vms); i++ {
			vm := vms[i]
			on, err := isPoweredOn(ctx, vm)
			if err != nil {
				msg := fmt.Sprintf("could not get power state for vm %q", vm.Name())
				log.Error(err, msg)

				status := createStatus(vmv1alpha1.PendingStatusPhase, msg, err, &current, desired)
				status.CurrentReplicas = &current
				vg.Status = status

				return ctrl.Result{RequeueAfter: defaultRequeue}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
			}

			if !on {
				eg.Go(func() error {
					lim.acquire()
					defer lim.release()
					msg := fmt.Sprintf("vm %q powered off, attempting to power on...", vm.Name())
					log.Info(msg)

					task, err := vm.PowerOn(egCtx)
					if err != nil {
						return err
					}

					return task.Wait(egCtx)
				})
			}
		}

		err = eg.Wait()
		if err != nil {
			msg := "could not power on virtual machine"
			log.Error(err, msg)

			status := createStatus(vmv1alpha1.PendingStatusPhase, msg, err, &current, desired)
			status.CurrentReplicas = &current
			vg.Status = status

			return ctrl.Result{RequeueAfter: defaultRequeue}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
		}
	}

	status := createStatus(vmv1alpha1.RunningStatusPhase, successMessage, nil, &current, desired)
	vg.Status = status

	// we're done, return successfully
	return ctrl.Result{}, errors.Wrap(r.Client.Status().Update(ctx, vg), "could not update status")
}

func (r *VmGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1alpha1.VmGroup{}).
		Complete(r)
}

func createStatus(phase vmv1alpha1.StatusPhase, msg string, err error, current *int32, desired int32) vmv1alpha1.VmGroupStatus {
	if err != nil {
		msg = msg + ": " + err.Error()
	}

	status := vmv1alpha1.VmGroupStatus{
		Phase:           phase,
		CurrentReplicas: current,
		DesiredReplicas: desired,
		LastMessage:     msg,
	}
	return status
}

// delete any external resources associated with the VmGroup
// Ensure that delete implementation is idempotent and safe to invoke
// multiple types for same object.
func (r *VmGroupReconciler) deleteExternalResources(ctx context.Context, finder *find.Finder, vg *vmv1alpha1.VmGroup) error {
	var nfe *find.NotFoundError

	// try to find the group folder
	groupName := getGroupName(vg.Namespace, vg.Name)
	group, err := getVMGroup(ctx, finder, groupName)
	if err != nil {
		if errors.As(err, &nfe) {
			// group already deleted, nothing to do
			return nil
		}
		return errors.Wrap(err, "could not get VmGroup")
	}

	// get replicas (VMs) for VmGroup
	vms, err := getReplicas(ctx, r.Finder, groupName)
	if err != nil {
		if errors.As(err, &nfe) {
			// all VMs already deleted, delete group folder
			msg := fmt.Sprintf("deleting group folder %q (path: %q)", groupName, group.InventoryPath)
			r.Log.Info(msg)
			if err := deleteFolder(ctx, group); err != nil {
				return errors.Wrap(err, "could not delete VmGroup")
			}
			return nil
		}
		return errors.Wrap(err, "could not delete VmGroup")
	}

	lim := newLimiter(defaultConcurrency)
	eg, egCtx := errgroup.WithContext(ctx) // used for concurrent operations against vCenter

	for i := 0; i < len(vms); i++ {
		lim.acquire()

		vm := vms[i]
		msg := fmt.Sprintf("deleting virtual machine %q", vm.Name())
		r.Log.Info(msg)

		eg.Go(func() error {
			defer lim.release()
			return deleteVM(egCtx, vm)
		})
	}

	err = eg.Wait()
	if err != nil {
		return errors.Wrap(err, "could not delete VmGroup")
	}

	// all VMs deleted, finally delete group folder
	msg := fmt.Sprintf("deleting group folder %q (path: %q)", groupName, group.InventoryPath)
	r.Log.Info(msg)
	return errors.Wrap(deleteFolder(ctx, group), "could not delete VmGroup")
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func generateName() string {
	b := make([]rune, defaultNameLength)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func getGroupName(namespace, name string) string {
	return fmt.Sprintf("%s-%s", namespace, name)
}
