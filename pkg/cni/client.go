package cni

import (
	"context"
	"github.com/containernetworking/cni/pkg/skel"
	cnipb "github.com/smartxworks/lynx/pkg/apis/cni/v1alpha1"
	"google.golang.org/grpc"
	"k8s.io/klog"
	"net"
	"os"
)

const CNISocketAddr = "/var/lib/lynx/cni.sock"

func rpcRequest(requestType string, arg *skel.CmdArgs) error {
	conn, err := grpc.Dial(CNISocketAddr,
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (conn net.Conn, e error) {
			unixAddr, err := net.ResolveUnixAddr("unix", CNISocketAddr)
			connUnix, err := net.DialUnix("unix", nil, unixAddr)
			return connUnix, err
		}))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := cnipb.NewCniClient(conn)

	cmdRequest := cnipb.CniRequest{

		ContainerId: arg.ContainerID,
		Ifname:      arg.IfName,
		Args:        arg.Args,
		Netns:       arg.Netns,
		Stdin:       arg.StdinData,
		Path:        arg.Path,
	}

	var resp *cnipb.CniResponse

	switch requestType {
	case "add":
		resp, err = client.CmdAdd(context.Background(), &cmdRequest)
	case "del":
		resp, err = client.CmdDel(context.Background(), &cmdRequest)
	case "check":
		resp, err = client.CmdCheck(context.Background(), &cmdRequest)

	}

	os.Stdout.Write(resp.Result)

	return err
}

func AddRequest(arg *skel.CmdArgs) error {
	klog.Info("Receive add:", arg)
	return rpcRequest("add", arg)
}

func DelRequest(arg *skel.CmdArgs) error {
	return rpcRequest("del", arg)
}

func CheckRequest(arg *skel.CmdArgs) error {
	return rpcRequest("check", arg)
}
