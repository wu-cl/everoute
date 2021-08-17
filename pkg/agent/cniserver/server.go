package cniserver

import (
	"bytes"
	"context"
	"fmt"
	cnipb "github.com/smartxworks/lynx/pkg/apis/cni/v1alpha1"
	"google.golang.org/grpc"
	"k8s.io/klog"
	"net"
	"os/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

const CNISocketAddr = "/var/lib/lynx/cni.sock"

type CNIServer struct {
	k8sClient client.Client
}

func (s *CNIServer) CmdAdd(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	klog.Info("Enter cmdadd")
	klog.Info(request)
	/*
		// create veth pair
		la := netlink.NewLinkAttrs()
		la.Name = "veth-" + request.Netns
		veth := netlink.Veth{
			LinkAttrs: la,
			PeerName:  "vethpeer-" + request.Netns,
		}
		// ip link add veth-xxx type veth peer name vethpeer-xxx
		if err := netlink.LinkAdd(&veth); err != nil {
			fmt.Errorf("Error Add Endpoint Device: %v", err)
		}
		// ip link set veth-xxx up
		if err := netlink.LinkSetUp(&veth); err != nil {
			fmt.Errorf("Error Add Endpoint Device: %v", err)
		}*/
	// parse requestArgs into map
	var portInfo map[string]string
	portInfo = make(map[string]string)
	for _, item := range strings.Split(request.Args, ";") {
		if item == "" {
			break
		}
		itemArr := strings.Split(item, "=")
		portInfo[itemArr[0]] = itemArr[1]
	}
	portExternalIDValue := "endpoint-" + portInfo["K8S_POD_NAMESPACE"] + "-" + portInfo["K8S_POD_NAME"]



	addCmd := fmt.Sprintf(`
		set -o errexit
		set -o nounset
		set -o xtrace

		netnsPath=%s
		netns=$(echo ${netnsPath} | awk -F '/' '{print $3}')
		mkdir -p /var/run/netns
		ln -s /host${netnsPath} /var/run/netns/${netns}
		bridgeName=%s
		ipAddr=%s
		ifName=%s

		vethName="veth-${netns}"
		portName=${vethName}
		vethPeerName="vethpeer-${netns}"
		portExternalIDName="pod-uuid"
		portExternalIDValue=%s

		ip link add ${vethName} type veth peer name ${vethPeerName}
		ip link set ${vethName} up

		ip link set ${vethPeerName} netns ${netns}
		ip netns exec ${netns} ip link set lo up
		ip netns exec ${netns} ip link set dev ${vethPeerName} name ${ifName}
		ip netns exec ${netns} ip link set ${ifName} up
		ip netns exec ${netns} ip a add ${ipAddr} dev ${ifName}

		attached_mac=$(ip netns exec ${netns} cat /sys/class/net/${ifName}/address)
		ovs-vsctl add-port ${bridgeName} ${portName} \
			-- set interface ${portName} external_ids=${portExternalIDName}=${portExternalIDValue} \
			-- set interface ${portName} external_ids:attached-mac="${attached_mac}"

		rm  /var/run/netns/${netns}
		
	`, request.Netns, "vlanLearnBridge", "10.0.0.1", request.Ifname, portExternalIDValue)

	cmd := exec.Command("/bin/sh", "-c", addCmd)

	var out bytes.Buffer
	var outErr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &outErr
	err := cmd.Run()

	klog.Info("cmd out:", out.String())
	klog.Info("cmd err:", outErr.String())

	return nil, err
}

func (s *CNIServer) CmdCheck(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	return nil, nil
}

func (s *CNIServer) CmdDel(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	return nil, nil
}

func (s *CNIServer) Initialize(k8sClient client.Client) {
	s.k8sClient = k8sClient

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
