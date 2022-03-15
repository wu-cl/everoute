package main

import (
	"context"
	"flag"
	agentv1alpha1 "github.com/everoute/everoute/pkg/apis/agent/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = agentv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: "0",
		Port:               9443,
	})
	if err != nil {
		klog.Fatalf("unable to create manager: %s", err.Error())
	}
	client := mgr.GetClient()

	var pods corev1.PodList
	err = client.List(context.Background(), &pods)
	if err != nil {
		klog.Error(err)
	}
	klog.Infof("%+v", pods)
}
