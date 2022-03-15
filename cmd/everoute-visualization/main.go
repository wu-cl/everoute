package main

import (
	"flag"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/everoute/everoute/pkg/apis/exporter/v1alpha1"
	"google.golang.org/protobuf/proto"
	"k8s.io/klog"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

var (
	hosts string
	topic string
)

func main() {
	flag.StringVar(&hosts, "host", "192.168.24.37:30777", "Kafka hosts")
	flag.StringVar(&topic, "topic", "sflow-counter", "Kafka topic")
	flag.Parse()

	consumer, err := sarama.NewConsumer(strings.Split(hosts, ","), sarama.NewConfig())
	if err != nil {
		log.Fatal(err)
	}
	ppp, err := consumer.Partitions(topic)

	wait := sync.WaitGroup{}

	var counter uint64 = 0

	for _, partition := range ppp {
		wait.Add(1)
		partition := partition
		go func() {
			p, err := consumer.ConsumePartition(topic, partition, sarama.OffsetNewest)
			if err != nil {
				log.Fatal(err)
			}
			for {
				select {
				case msg := <-p.Messages():
					/*
						mm := &v1alpha1.FlowMessage{}
						_ = proto.Unmarshal(msg.Value, mm)
						if len(mm.Flow) > 0 {
								for _,item := range mm.Flow{
									if net.IP(item.OriginTuple.Src).String() == "10.244.2.30"{
										klog.Info(item)
									}
								}


						}

					*/

					mm := &v1alpha1.CounterMessage{}
					_ = proto.Unmarshal(msg.Value, mm)
					for _, c := range mm.Counter {
						klog.Info(c)
					}

					/*

						mm := &v1alpha1.FlowMessage{}
						_ = proto.Unmarshal(msg.Value, mm)
						for _, f := range mm.Flow {

							klog.Infof("%s:%d -> %s:%d policy: %s", net.IP(f.OriginTuple.Src).String(), f.OriginTuple.SrcPort, net.IP(f.OriginTuple.Dst).String(), f.OriginTuple.DstPort, f.Policy)


								if f.Protocol == uint32(layers.IPProtocolTCP) {
									if f.ProtocolInfo != nil {
										klog.Infof("%s:%d -> %s:%d rtt:%d label:%#x label_mask:%#x", net.IP(f.OriginTuple.Src).String(), f.OriginTuple.SrcPort, net.IP(f.OriginTuple.Dst).String(), f.OriginTuple.DstPort, f.ProtocolInfo.TcpInfo.Rtt,f.CtLabel,f.CtLabelMask)
									} else {
										klog.Infof("%s:%d -> %s:%d empty", net.IP(f.OriginTuple.Src).String(), f.OriginTuple.SrcPort, net.IP(f.OriginTuple.Dst).String(), f.OriginTuple.DstPort)
									}
								}


						}
					*/

				}
			}

			wait.Done()
		}()
	}

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println(counter)
		os.Exit(0)
	}()

	wait.Wait()
}
