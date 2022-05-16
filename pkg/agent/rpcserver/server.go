/*
Copyright 2021 The Everoute Authors.

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

package rpcserver

import (
	"context"
	"net"
	"os"

	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog"

	"github.com/everoute/everoute/pkg/agent/datapath"
	pb "github.com/everoute/everoute/pkg/apis/rpc/v1alpha1"
)

const RPCSocketAddr = "/var/run/everoute/rpc.sock"

type Server struct {
	dpManager *datapath.DpManager
	stopChan  <-chan struct{}
}

type Test struct {
}

func (t *Test) TestMethod(ctx context.Context, req *emptypb.Empty) (*pb.ArpResponse, error) {
	klog.Info("TestMethod")
	return &pb.ArpResponse{
		Pkt: []byte{1, 1},
	}, nil
}

func Initialize(datapathManager *datapath.DpManager) *Server {
	s := &Server{
		dpManager: datapathManager,
	}

	return s
}

func (s *Server) Run(stopChan <-chan struct{}) {
	klog.Info("Starting Collector server")
	s.stopChan = stopChan

	// remove the remaining sock file
	_, err := os.Stat(RPCSocketAddr)
	if err == nil {
		err = os.Remove(RPCSocketAddr)
		if err != nil {
			klog.Fatalf("remove remaining cni sock file error, err:%s", err)
			return
		}
	}

	// listen socket
	listener, err := net.Listen("unix", RPCSocketAddr)
	if err != nil {
		klog.Fatalf("Failed to bind on %s: %v", RPCSocketAddr, err)
	}

	// register collector service
	rpcServer := grpc.NewServer()
	collector := NewCollectorServer(s.dpManager, stopChan)
	pb.RegisterCollectorServer(rpcServer, collector)

	pb.RegisterTestServer(rpcServer, &Test{})

	go func() {
		if err = rpcServer.Serve(listener); err != nil {
			klog.Fatalf("Failed to serve collectorServer connections: %v", err)
		}
	}()

	klog.Info("rpc server is listening ...")
	<-s.stopChan
}
