package main

import (
	"flag"
	"fmt"
	"github.com/Shopify/sarama"
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
	flag.StringVar(&hosts, "host", "192.168.26.10:9092,192.168.28.177:9092,192.168.30.12:9092", "Kafka hosts")
	flag.StringVar(&topic, "topic", "test", "Kafka topic")
	flag.Parse()

	consumer, err := sarama.NewConsumer(strings.Split(hosts, ","), sarama.NewConfig())
	if err != nil {
		log.Fatal(err)
	}
	ppp, err := consumer.Partitions(topic)

	wait := sync.WaitGroup{}

	mutex := sync.Mutex{}

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
					//mm := &v1alpha1.FlowMessage{}
					//_ = proto.Unmarshal(msg.Value, mm)
					fmt.Println(msg.Timestamp)
					mutex.Lock()
					counter += uint64(len(msg.Value))
					mutex.Unlock()
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
