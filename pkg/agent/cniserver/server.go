package cniserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	cnipb "github.com/smartxworks/lynx/pkg/apis/cni/v1alpha1"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"net"
	"os"
	"os/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

const CNISocketAddr = "/var/lib/lynx/cni.sock"

type CNIServer struct {
	k8sClient client.Client
	podCIDR   []types.IPNet
}

type CNIArgs struct {
	types.CommonArgs
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

func (s *CNIServer) CmdAdd(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	klog.Info("Enter cmdadd")
	klog.Info(request)

	conf := types.NetConf{}
	json.Unmarshal(request.Stdin, &conf)
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
	args := &CNIArgs{}
	err := types.LoadArgs(request.Args, args)
	if err != nil {
		return nil, err
	}

	portExternalIDValue := "endpoint-" + args.K8S_POD_NAMESPACE + "-" + args.K8S_POD_NAME

	klog.Error(err)
	ipip := allocator.Net{
		Name:       conf.Name,
		CNIVersion: conf.CNIVersion,
		IPAM: &allocator.IPAMConfig{
			Type:    "host-local",
			Ranges:  []allocator.RangeSet{append(allocator.RangeSet{}, allocator.Range{Subnet: s.podCIDR[0]})},
			DataDir: "/tmp/cni-example",
		},
		Args: nil,
	}
	os.Setenv("CNI_PATH", request.Path)
	os.Setenv("CNI_CONTAINERID", request.ContainerId)
	os.Setenv("CNI_NETNS", request.Netns)
	os.Setenv("CNI_IFNAME", request.Ifname)
	ipipByte, _ := json.Marshal(ipip)
	r, err := ipam.ExecAdd("host-local", ipipByte)
	klog.Error(err)
	ipamResult, err := types100.NewResultFromResult(r)
	klog.Error(err)
	klog.Infof("ipamResult:%s", ipamResult)

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

		portExternalIDName="pod-uuid"
		portExternalIDValue=%s

		vethName="veth-${netns}"
		portName=${vethName}
		vethPeerName="vethpeer-${netns}"
		
		gateway=%s

		ip link add ${vethName} type veth peer name ${vethPeerName}
		ip link set ${vethName} up

		ip link set ${vethPeerName} netns ${netns}
		ip netns exec ${netns} ip link set lo up
		ip netns exec ${netns} ip link set dev ${vethPeerName} name ${ifName}
		ip netns exec ${netns} ip link set ${ifName} up
		ip netns exec ${netns} ip a add ${ipAddr} dev ${ifName}
		ip netns exec ${netns} ip route add default via ${gateway}

		attached_mac=$(ip netns exec ${netns} cat /sys/class/net/${ifName}/address)
		ovs-vsctl add-port ${bridgeName} ${portName} \
			-- set interface ${portName} external_ids=${portExternalIDName}=${portExternalIDValue} \
			-- set interface ${portName} external_ids:attached-mac="${attached_mac}"

		rm  /var/run/netns/${netns}
		
		#ip a del ${gateway}/32 dev gw0
		#ip a add ${gateway}/32 dev gw0
	`, request.Netns, "vlanLearnBridge", ipamResult.IPs[0].Address.String(),
		request.Ifname, portExternalIDValue, ipamResult.IPs[0].Gateway.String())

	cmd := exec.Command("/bin/sh", "-c", addCmd)

	var out bytes.Buffer
	var outErr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &outErr
	err = cmd.Run()

	klog.Info("cmd out:", out.String())
	klog.Info("cmd err:", outErr.String())

	_, ipnet, err := net.ParseCIDR(ipamResult.IPs[0].Address.String())
	klog.Error(err)

	resp := types100.Result{
		CNIVersion: conf.CNIVersion,
		IPs: []*types100.IPConfig{&types100.IPConfig{
			Address: *ipnet,
		}},
	}
	var resultBytes bytes.Buffer
	err = resp.PrintTo(&resultBytes)
	klog.Error(err)
	return &cnipb.CniResponse{
		Result: resultBytes.Bytes(),
		Error:  nil,
	}, err
}

func (s *CNIServer) CmdCheck(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	return nil, nil
}

func (s *CNIServer) CmdDel(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	klog.Info(request)
	conf := types.NetConf{}

	// delete ovs port
	addCmd := fmt.Sprintf(`
		set -o errexit
		set -o nounset
		set -o xtrace

		netnsPath=%s
		netns=$(echo ${netnsPath} | awk -F '/' '{print $3}')

		ovs-vsctl del-port %s veth-{netns}
		`, request.Netns, "vlanLearnBridge")

	cmd := exec.Command("/bin/sh", "-c", addCmd)

	var out bytes.Buffer
	var outErr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &outErr
	err := cmd.Run()

	klog.Info("cmd out:", out.String())
	klog.Info("cmd err:", outErr.String())

	// release allocated IP
	ipip := allocator.Net{
		Name:       conf.Name,
		CNIVersion: conf.CNIVersion,
		IPAM: &allocator.IPAMConfig{
			Type:    "host-local",
			Ranges:  []allocator.RangeSet{append(allocator.RangeSet{}, allocator.Range{Subnet: s.podCIDR[0]})},
			DataDir: "/tmp/cni-example",
		},
		Args: nil,
	}
	os.Setenv("CNI_PATH", request.Path)
	os.Setenv("CNI_CONTAINERID", request.ContainerId)
	os.Setenv("CNI_NETNS", request.Netns)
	os.Setenv("CNI_IFNAME", request.Ifname)
	ipipByte, _ := json.Marshal(ipip)
	err = ipam.ExecDel("host-local", ipipByte)
	klog.Error(err)

	resp := types100.Result{
		CNIVersion: conf.CNIVersion,
	}
	var resultBytes bytes.Buffer
	err = resp.PrintTo(&resultBytes)
	klog.Error(err)
	return &cnipb.CniResponse{
		Result: resultBytes.Bytes(),
		Error:  nil,
	}, err
}

func (s *CNIServer) Initialize(k8sClient client.Client) {
	s.k8sClient = k8sClient

	// TODO: sync all endpoints info

	// get node CIDR
	// TODO: hostname cannot be modified after joining into cluster
	nodeName, _ := os.Hostname()
	nodeName = strings.ToLower(nodeName)
	node := corev1.Node{}
	key := client.ObjectKey{
		Name: nodeName,
	}
	err := s.k8sClient.Get(context.Background(), key, &node)
	klog.Error(err)
	klog.Infof("node PodCIDRs:%s", node.Spec.PodCIDRs)
	for _, cidrString := range node.Spec.PodCIDRs {
		cidr, _ := types.ParseCIDR(cidrString)
		s.podCIDR = append(s.podCIDR, types.IPNet(*cidr))
	}

	// set gateway ip address
	gwIP := make([]byte, 4)
	copy(gwIP, s.podCIDR[0].IP.To4())
	gwIP[3] = gwIP[3] + 1

	gw := net.IPNet{
		IP:   gwIP,
		Mask: s.podCIDR[0].Mask,
	}
	klog.Info(gw.String())
	cmd := fmt.Sprintf(`
		set -o errexit
		set -o nounset
		set -o xtrace

		ip a add %s dev gw0
	`, gw.String())

	result := exec.Command("/bin/sh", "-c", cmd)

	var out bytes.Buffer
	var outErr bytes.Buffer
	result.Stdout = &out
	result.Stderr = &outErr
	err = result.Run()

	klog.Info("cmd out:", out.String())
	klog.Info("cmd err:", outErr.String())

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
