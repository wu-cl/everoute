package main

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NetworkPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}
