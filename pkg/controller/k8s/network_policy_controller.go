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

package k8s

import (
	"context"
	"fmt"
	"github.com/smartxworks/lynx/pkg/apis/security/v1alpha1"
	lynxctrl "github.com/smartxworks/lynx/pkg/controller"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// NetworkPolicyReconciler watch k8s and sync to endpoint
type NetworkPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile receive networkpolicy and sync to securitypolicy
func (r *NetworkPolicyReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	klog.Infof("PodReconciler received k8s %s reconcile", req.NamespacedName)

	pod := v1.Pod{}

	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {

		klog.Infof("Enter k8s delete")
		endpointReq := types.NamespacedName{
			Name: "endpoint-" + req.Namespace + "-" + req.Name,
		}

		klog.Info(endpointReq)
		endpoint := v1alpha1.Endpoint{}
		if err := r.Get(ctx, endpointReq, &endpoint); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if err = r.Delete(ctx, &endpoint); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}


	return ctrl.Result{}, nil
}

// SetupWithManager create and add Endpoint Controller to the manager.
func (r *NetworkPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if mgr == nil {
		return fmt.Errorf("can't setup with nil manager")
	}

	c, err := controller.New("network-policy-controller", mgr, controller.Options{
		MaxConcurrentReconciles: lynxctrl.DefaultMaxConcurrentReconciles,
		Reconciler:              r,
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &v1.Pod{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}
