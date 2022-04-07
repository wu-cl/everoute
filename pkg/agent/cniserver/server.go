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

package cniserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	"github.com/contiv/ofnet/ovsdbDriver"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/everoute/everoute/pkg/agent/datapath"
	cnipb "github.com/everoute/everoute/pkg/apis/cni/v1alpha1"
)

const CNISocketAddr = "/var/run/everoute/cni.sock"

const (
	vethPrefix    = "veth"
	vrfPrefix     = "vrf"
	dataIfaceName = "ens4"
)

type CNIServer struct {
	k8sClient client.Client
	ovsDriver *ovsdbDriver.OvsDriver
	gwName    string
	podCIDR   []cnitypes.IPNet

	mutex sync.Mutex
}

type CNIArgs struct {
	cnitypes.CommonArgs
	K8S_POD_NAME               cnitypes.UnmarshallableString //nolint
	K8S_POD_NAMESPACE          cnitypes.UnmarshallableString //nolint
	K8S_POD_INFRA_CONTAINER_ID cnitypes.UnmarshallableString //nolint
}

func (s *CNIServer) ParseConf(request *cnipb.CniRequest) (*cnitypes.NetConf, *CNIArgs, error) {
	// parse request Stdin
	conf := &cnitypes.NetConf{}
	err := json.Unmarshal(request.Stdin, &conf)
	if err != nil {
		return nil, nil, err
	}

	// parse request Args
	args := &CNIArgs{}
	err = cnitypes.LoadArgs(request.Args, args)
	if err != nil {
		return nil, nil, err
	}

	return conf, args, err
}

func (s *CNIServer) ParseResult(result *cniv1.Result) (*cnipb.CniResponse, error) {
	// convert result to target version
	newResult, err := result.GetAsVersion(result.CNIVersion)
	if err != nil {
		klog.Errorf("get target version error, err: %s", err)
		return s.RetError(cnipb.ErrorCode_INCOMPATIBLE_CNI_VERSION, "get target version error", err)
	}

	var resultBytes bytes.Buffer
	if err = newResult.PrintTo(&resultBytes); err != nil {
		klog.Errorf("can not convert result automatically, err: %s", err)
		return s.RetError(cnipb.ErrorCode_INCOMPATIBLE_CNI_VERSION, "can not convert result automatically", err)
	}

	return &cnipb.CniResponse{
		Result: resultBytes.Bytes(),
		Error:  nil,
	}, nil
}

func (s *CNIServer) CmdAdd(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	klog.Infof("Create new pod %s", request)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	conf, args, err := s.ParseConf(request)
	if err != nil {
		klog.Errorf("Parse request conf error, err: %s", err)
		return s.RetError(cnipb.ErrorCode_DECODING_FAILURE, "Parse request conf error", err)
	}

	// create cni result structure
	ipA, ipNet, _ := net.ParseCIDR(string(args.K8S_POD_NAME) + "/20")
	result := &cniv1.Result{
		CNIVersion: conf.CNIVersion,
		IPs: []*cniv1.IPConfig{{
			Address: net.IPNet{
				IP:   ipA,
				Mask: ipNet.Mask,
			},
			Gateway: net.ParseIP("192.168.64.1"),
		},
		},
		Routes: []*cnitypes.Route{{
			Dst: net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.IPMask(net.IPv4zero)},
			GW: net.ParseIP("192.168.64.1")}},
		Interfaces: []*cniv1.Interface{{
			Name:    request.Ifname,
			Sandbox: request.Netns}},
	}
	// set the correspondence between interface and ip address
	result.IPs[0].Interface = cniv1.Int(0)

	nsPath := "/host" + request.Netns
	// vethName - ovs port name
	vethBase := uuidToNum(request.ContainerId)
	vethName := fmt.Sprintf("%s%d", vethPrefix, vethBase)
	if err = ns.WithNetNSPath(nsPath, func(hostNS ns.NetNS) error {
		// create veth pair in container NS and host NS
		// TODO: MTU is a const variable here
		_, containerVeth, err := ip.SetupVethWithName(request.Ifname, vethName, 1500, "", hostNS)
		if err != nil {
			klog.Errorf("create veth device error, err: %s", err)
			return err
		}
		result.Interfaces[0].Mac = containerVeth.HardwareAddr.String()
		if err = ipam.ConfigureIface(request.Ifname, result); err != nil {
			klog.Errorf("configure ip address in container error, err: %s", err)
			return err
		}
		return nil
	}); err != nil {
		return s.RetError(cnipb.ErrorCode_IO_FAILURE, "exec error in namespace", err)
	}

	// Generate vrfID from vethBase
	vrfID := vethBase

	// Create Vrf
	vrfName := fmt.Sprintf("%s%d", vrfPrefix, vrfID)
	vrf := &netlink.Vrf{
		LinkAttrs: netlink.LinkAttrs{
			Name: vrfName,
		},
		Table: vrfID,
	}
	netlink.LinkAdd(vrf)

	// Get uplink and vethpair by name
	uplink, _ := netlink.LinkByName(dataIfaceName)
	vethlink, _ := netlink.LinkByName(vethName)

	// Create MacVlan Inferface and Master to vrf
	podMac, _ := net.ParseMAC(result.Interfaces[0].Mac)
	macVlan := &netlink.Macvlan{
		LinkAttrs: netlink.LinkAttrs{

			Name:         fmt.Sprintf("%s.%d", dataIfaceName, vethBase),
			ParentIndex:  uplink.Attrs().Index,
			MasterIndex:  vrf.Index,
			HardwareAddr: podMac,
		},
		Mode: netlink.MACVLAN_MODE_VEPA,
	}
	netlink.LinkAdd(macVlan)

	// Master veth pair to vrf
	netlink.LinkSetMaster(vethlink, vrf)

	// Set links up
	netlink.LinkSetUp(macVlan)
	netlink.LinkSetUp(vrf)

	// Set IP address
	netlink.AddrAdd(macVlan, &netlink.Addr{
		IPNet: &result.IPs[0].Address,
	})
	fakeAddr, _ := netlink.ParseAddr("100.100.1.1/24")
	netlink.AddrAdd(vethlink, fakeAddr)

	// Set sysCtl
	cmd := "echo 1 > /host/proc/sys/net/ipv4/conf/" + vethName + "/proxy_arp"
	exec.Command("/usr/bin/env", "bash", "-c", cmd).Run()

	cmd = "echo 0 > /host/proc/sys/net/ipv4/conf/" + vethName + "/rp_filter"
	exec.Command("/usr/bin/env", "bash", "-c", cmd).Run()

	cmd = "echo 1 > /host/proc/sys/net/ipv4/conf/" + vethName + "/accept_local"
	exec.Command("/usr/bin/env", "bash", "-c", cmd).Run()

	cmd = "echo 1 > /host/proc/sys/net/ipv4/conf/" + macVlan.Name + "/proxy_arp"
	exec.Command("/usr/bin/env", "bash", "-c", cmd).Run()

	// Set arptables
	cmd = "num=`arptables -L INPUT | grep \"i " + macVlan.Name + "\" | grep ACCEPT | wc -l`;[[ $num -eq 0 ]] " +
		"&& arptables -I INPUT 1 -j ACCEPT -i " + macVlan.Name + " -d " + ipA.String()
	exec.Command("/usr/bin/env", "bash", "-c", cmd).Run()

	// Clean route table item
	routes, err := netlink.RouteListFiltered(unix.AF_INET, &netlink.Route{Table: int(vrfID)}, netlink.RT_FILTER_TABLE)
	if err != nil {
		fmt.Print(err)
	}
	for _, item := range routes {
		netlink.RouteDel(&item)
	}

	// Route for same network
	if err := netlink.RouteAdd(&netlink.Route{
		Dst:       ipNet,
		LinkIndex: macVlan.Index,
		Table:     int(vrfID),
		Scope:     netlink.SCOPE_LINK,
	}); err != nil {
		fmt.Print(err)
	}

	// Route for default
	if err := netlink.RouteAdd(&netlink.Route{
		Dst:       nil,
		Gw:        net.ParseIP("192.168.64.215"),
		LinkIndex: macVlan.Index,
		Table:     int(vrfID),
	}); err != nil {
		fmt.Print(err)
	}

	// Route for back to pod
	if err := netlink.RouteAdd(&netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   ipA.To4(),
			Mask: net.IPMask(net.IPv4bcast.To4()),
		},
		LinkIndex: vethlink.Attrs().Index,
		Table:     int(vrfID),
	}); err != nil {
		fmt.Print(err)
	}

	return s.ParseResult(result)
}

func Ipv4HostToInt(ipNet net.IPNet) uint32 {
	ipAddr := binary.BigEndian.Uint32(ipNet.IP.To4())
	ipMask := binary.BigEndian.Uint32(ipNet.Mask)
	ipMaskR := ^ipMask

	ipHost := ipAddr & ipMaskR

	return ipHost
}

func uuidToNum(uuid string) uint32 {
	// return value lead with 1
	var ret uint32 = 1

	count := 0

	for _, char := range uuid {
		if count == 8 {
			break
		}
		num := int32(char - '0')
		if num >= 0 && num <= 9 {
			ret = ret*10 + uint32(num)
			count++
		}
	}

	return ret
}

func (s *CNIServer) CmdCheck(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	klog.Infof("Check pod %s", request)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	return &cnipb.CniResponse{Result: []byte("")}, nil
}

func (s *CNIServer) CmdDel(ctx context.Context, request *cnipb.CniRequest) (*cnipb.CniResponse, error) {
	klog.Infof("Delete pod %s", request)
	klog.Info(request)

	vethBase := uuidToNum(request.ContainerId)
	vethName := fmt.Sprintf("%s%d", vethPrefix, vethBase)
	vrfName := fmt.Sprintf("%s%d", vrfPrefix, vethBase)
	macVlanName := fmt.Sprintf("%s.%d", dataIfaceName, vethBase)

	if vethLink, err := netlink.LinkByName(vethName); err == nil {
		netlink.LinkDel(vethLink)
	}
	if vrfLink, err := netlink.LinkByName(vrfName); err == nil {
		netlink.LinkDel(vrfLink)
	}
	if macVlanLink, err := netlink.LinkByName(macVlanName); err == nil {
		netlink.LinkDel(macVlanLink)
	}

	/*
		vethBase := request.ContainerId[:10]
		vethName := vethPrefix + vethBase
		vrfID := Ipv4HostToInt(result.IPs[0].Address) + vrfOffset
		vrfName:=fmt.Sprintf("%s%d", vrfPrefix, vrfID)*/

	return &cnipb.CniResponse{Result: []byte("")}, nil
}

func (s *CNIServer) RetError(code cnipb.ErrorCode, msg string, err error) (*cnipb.CniResponse, error) {
	resp := &cnipb.CniResponse{
		Result: nil,
		Error: &cnipb.Error{
			Code:    code,
			Message: msg,
			Details: err.Error(),
		},
	}
	return resp, err
}

func (s *CNIServer) GetIpamConfByte(conf *cnitypes.NetConf) []byte {
	var ipamRanges allocator.RangeSet
	for _, item := range s.podCIDR {
		ipamRanges = append(ipamRanges, allocator.Range{Subnet: item})
	}

	ipamConf := allocator.Net{
		Name:       conf.Name,
		CNIVersion: conf.CNIVersion,
		IPAM: &allocator.IPAMConfig{
			Type:   "host-local",
			Ranges: []allocator.RangeSet{ipamRanges},
		},
		Args: nil,
	}
	ipamByte, _ := json.Marshal(ipamConf)

	return ipamByte
}

func SetEnv(request *cnipb.CniRequest) {
	os.Setenv("CNI_PATH", request.Path)
	os.Setenv("CNI_CONTAINERID", request.ContainerId)
	os.Setenv("CNI_NETNS", request.Netns)
	os.Setenv("CNI_IFNAME", request.Ifname)
}

func SetLinkAddr(ifname string, inet *net.IPNet) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		klog.Errorf("failed to lookup %q: %v", ifname, err)
		return err
	}
	if err = netlink.LinkSetUp(link); err != nil {
		klog.Errorf("failed to set %q UP: %v", ifname, err)
		return err
	}
	addr := &netlink.Addr{
		IPNet: inet,
		Label: ""}
	if err = netlink.AddrAdd(link, addr); err != nil {
		klog.Errorf("failed to add IP addr to %s: %v", ifname, err)
		return err
	}
	return nil
}

func Initialize(k8sClient client.Client, datapathManager *datapath.DpManager) *CNIServer {
	s := &CNIServer{
		k8sClient: k8sClient,
		gwName:    datapathManager.AgentInfo.GatewayName,
		ovsDriver: datapathManager.OvsdbDriverMap[datapathManager.AgentInfo.BridgeName][datapath.LOCAL_BRIDGE_KEYWORD],
		podCIDR:   append([]cnitypes.IPNet{}, datapathManager.AgentInfo.PodCIDR...),
	}

	return s
}

func (s *CNIServer) Run(stopChan <-chan struct{}) {
	klog.Info("Starting CNI server")

	// remove the remaining sock file
	_, err := os.Stat(CNISocketAddr)
	if err == nil {
		err = os.Remove(CNISocketAddr)
		if err != nil {
			klog.Fatalf("remove remaining cni sock file error, err:%s", err)
			return
		}
	}

	// listen and start rpcServer
	listener, err := net.Listen("unix", CNISocketAddr)
	if err != nil {
		klog.Fatalf("Failed to bind on %s: %v", CNISocketAddr, err)
	}
	rpcServer := grpc.NewServer()
	cnipb.RegisterCniServer(rpcServer, s)
	go func() {
		if err = rpcServer.Serve(listener); err != nil {
			klog.Fatalf("Failed to serve connections: %v", err)
		}
	}()

	klog.Info("CNI server is listening ...")
	<-stopChan
}
