/*
Copyright 2021 The Lynx Authors.

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

package pod

import (
	"context"
	"fmt"
	"github.com/smartxworks/lynx/pkg/apis/security/v1alpha1"
	lynxctrl "github.com/smartxworks/lynx/pkg/controller"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PodReconciler watch pod and sync to endpoint
type PodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile receive endpoint from work queue, synchronize the endpoint status
// from agentinfo.
func (r *PodReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	klog.Infof("PodReconciler received endpoint %s reconcile", req.NamespacedName)

	pod := v1.Pod{}

	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		klog.Errorf("unable to fetch Pod %s: %s", req.Name, err.Error())
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if pod.ObjectMeta.DeletionTimestamp == nil && len(pod.ObjectMeta.Finalizers) == 0 {
		// new Pod, except host network
		if pod.Spec.HostNetwork == false {
			endpoint := v1alpha1.Endpoint{}
			// use endpoint-pod.name as endpoint name
			endpoint.Name = "endpoint-" + pod.Namespace + "-" + pod.Name
			// endpoint.Namespace = pod.Namespace
			// endpoint.Status.IPs = append(endpoint.Status.IPs, types.IPAddress(pod.Status.PodIP))
			endpoint.Spec.VID = 0
			endpoint.Spec.Reference.ExternalIDName = "pod-uuid"
			endpoint.Spec.Reference.ExternalIDValue = endpoint.Name

			// submit creation
			err := r.Create(ctx, &endpoint)
			if err != nil {
				klog.Errorf("create endpoint err: %s", err)
				return ctrl.Result{}, err
			}
		}

	} else if pod.ObjectMeta.DeletionTimestamp != nil {
		// delete Pod
		req.NamespacedName.Name = "endpoint-" + string(pod.UID)
		endpoint := v1alpha1.Endpoint{}
		err := r.Get(ctx, req.NamespacedName, &endpoint)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.Delete(ctx, &endpoint)
		if err != nil {
			return ctrl.Result{}, err
		}

	} else {
		// update Pod
	}

	return ctrl.Result{}, nil
}

// SetupWithManager create and add Endpoint Controller to the manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if mgr == nil {
		return fmt.Errorf("can't setup with nil manager")
	}

	c, err := controller.New("pod-controller", mgr, controller.Options{
		MaxConcurrentReconciles: lynxctrl.DefaultMaxConcurrentReconciles,
		Reconciler:              r,
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &v1.Pod{}}, &handler.Funcs{
		CreateFunc: r.addPod,
		DeleteFunc: r.deletePod,
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *PodReconciler) addPod(e event.CreateEvent, q workqueue.RateLimitingInterface) {
	if e.Meta == nil {
		klog.Errorf("addPod received with no metadata event: %v", e)
		return
	}

	q.Add(ctrl.Request{NamespacedName: k8stypes.NamespacedName{
		Namespace: e.Meta.GetNamespace(),
		Name:      e.Meta.GetName(),
	}})
}

func (r *PodReconciler) deletePod(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if e.Meta == nil {
		klog.Errorf("AddEndpoint received with no metadata event: %v", e)
		return
	}

	q.Add(ctrl.Request{NamespacedName: k8stypes.NamespacedName{
		Namespace: e.Meta.GetNamespace(),
		Name:      e.Meta.GetName(),
	}})
}
