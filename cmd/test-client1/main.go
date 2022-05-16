package main

import (
	"context"
	"fmt"
	"log"
	"net"

	pb "github.com/everoute/everoute/pkg/apis/rpc/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

const SocketAddr = "/var/run/everoute/rpc.sock"

func main() {

	// dial server

	conn, err := grpc.Dial(SocketAddr,
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (conn net.Conn, e error) {
			unixAddr, _ := net.ResolveUnixAddr("unix", SocketAddr)
			connUnix, err := net.DialUnix("unix", nil, unixAddr)
			return connUnix, err
		}))

	if err != nil {
		log.Println("wait collector socket server")

	}

	// create
	client := pb.NewTestClient(conn)
	resp, err := client.TestMethod(context.Background(), &emptypb.Empty{})
	if err != nil {
		fmt.Print(err)
	}
	fmt.Print(resp)

}
