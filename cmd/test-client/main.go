package main

import (
	"context"
	"io"
	"log"
	"net"
	"time"

	pb "github.com/everoute/everoute/pkg/apis/rpc/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

const CollectorSocketAddr = "/var/run/everoute/rpc.sock"

func main() {

	// dial server

	for {
		conn, err := grpc.Dial(CollectorSocketAddr,
			grpc.WithInsecure(),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (conn net.Conn, e error) {
				unixAddr, _ := net.ResolveUnixAddr("unix", CollectorSocketAddr)
				connUnix, err := net.DialUnix("unix", nil, unixAddr)
				return connUnix, err
			}))

		if err != nil {
			log.Println("wait collector socket server")
			time.Sleep(time.Second * 1)
			continue
		}

		// create stream
		client := pb.NewCollectorClient(conn)
		stream, err := client.ArpStream(context.Background(), &emptypb.Empty{})
		if err != nil {
			log.Println("wait collector socket server")
			time.Sleep(time.Second * 1)
			continue
		}

		recvHandle(stream)
	}

}

func recvHandle(stream pb.Collector_ArpStreamClient) {
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("cannot receive %v", err)
			break
		}
		log.Printf("Resp received: %s", resp.Pkt)
	}

	log.Printf("finished")
}
