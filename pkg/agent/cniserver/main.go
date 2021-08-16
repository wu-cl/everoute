package cniserver

import (
	"context"
	cnipb "github.com/smartxworks/lynx/pkg/apis/cni/v1alpha1"
	"google.golang.org/grpc"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"net"
)

const CNISocketAddr = "/var/lib/lynx/cni.sock"

type CNIServer struct {
	kubeClient clientset.Interface
}

func (s *CNIServer) CmdAdd(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	panic("implement me")
}

func (s *CNIServer) CmdCheck(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	panic("implement me")
}

func (s *CNIServer) CmdDel(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	panic("implement me")
}

func (s *CNIServer) Initialize(kubeClient clientset.Interface) {
	s.kubeClient = kubeClient

	// TODO: sync all endpoints info
}

func (s *CNIServer) Run(stopChan <-chan struct{}) {

	klog.Info("Starting CNI server")
	defer klog.Info("Shutting down CNI server")

	listener, err := net.Listen("unix", CNISocketAddr)
	if err != nil {
		klog.Fatalf("Failed to bind on %s: %v", CNISocketAddr, err)
	}
	rpcServer := grpc.NewServer()

	cnipb.RegisterCniServer(rpcServer, s)

	klog.Info("CNI server is listening ...")
	go func() {
		if err := rpcServer.Serve(listener); err != nil {
			klog.Errorf("Failed to serve connections: %v", err)
		}
	}()

	<-stopChan
}
