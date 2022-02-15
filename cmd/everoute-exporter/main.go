package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/ti-mo/conntrack"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/everoute/everoute/pkg/apis/exporter/v1alpha1"
)

var (
	hosts      string
	topic      string
	listenPort int
	agentID    string

	producer   sarama.AsyncProducer
	err        error
	sFlowCache *SFlowCache

	uplinkPort1 uint32
	uplinkPort2 uint32
	portName    map[int]string

	stopChan chan struct{}
)

type SFlowCache struct {
	mutex sync.Mutex

	ipMac map[string][]byte

	arpSend []layers.ARP
	arpRecv []layers.ARP

	sFlowChan chan layers.SFlowDatagram
}

func (s *SFlowCache) New(c chan layers.SFlowDatagram) *SFlowCache {
	cache := &SFlowCache{
		sFlowChan: c,
	}
	cache.ipMac = make(map[string][]byte)
	return cache
}

func ParseByteIPv4(ip []byte) net.IP {
	return net.IPv4(ip[0], ip[1], ip[2], ip[3])
}

func (s *SFlowCache) AddArp(arp layers.ARP) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.arpSend = append(s.arpSend, arp)
	s.ipMac[ParseByteIPv4(arp.SourceProtAddress).String()] = arp.SourceHwAddress
}

func (s *SFlowCache) GetMac(ip string) []byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.ipMac[ip]

}

func (s *SFlowCache) AddIp(pkt gopacket.Packet) {
	eth := layers.Ethernet{}
	err := eth.DecodeFromBytes(pkt.LinkLayer().LayerContents(), gopacket.NilDecodeFeedback)
	if err != nil {
		return
	}

	ip := layers.IPv4{}
	err = ip.DecodeFromBytes(pkt.LinkLayer().LayerPayload(), gopacket.NilDecodeFeedback)
	if err != nil {
		return
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.ipMac[ParseByteIPv4(ip.SrcIP).String()] = eth.SrcMAC
}

func main() {
	flag.StringVar(&hosts, "host", "192.168.26.10:9092,192.168.28.177:9092,192.168.30.12:9092", "Kafka hosts")
	flag.StringVar(&topic, "topic", "test", "Kafka topic")
	flag.IntVar(&listenPort, "port", 2345, "Listen port")
	flag.StringVar(&agentID, "agent-id", "", "Agent ID")
	flag.Parse()

	stopChan = make(chan struct{})
	ctChan := make(chan []conntrack.Flow, 100)
	sFlowChan := make(chan layers.SFlowDatagram, 100)

	portName = make(map[int]string)

	uplinkPort1 = 9
	uplinkPort2 = 10

	producer, err = sarama.NewAsyncProducer(strings.Split(hosts, ","), sarama.NewConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer producer.Close()

	sFlowCache = sFlowCache.New(sFlowChan)

	// catch ctrl + c
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)
	go func() {
		<-signals
		log.Printf("Caught signal, stopping")
		close(stopChan)
	}()

	err = configSflowForOVS(listenPort)
	if err != nil {
		log.Fatal(err)
	}
	defer mustCleanOVSConfig()

	go ConntrackCollecter(ctChan, stopChan)
	go ConntrackWorker(ctChan, sFlowCache, stopChan)

	go SFlowCollector(sFlowChan, stopChan)
	go SFlowWorker(sFlowChan, sFlowCache, stopChan)

	go KafkaErrorCheck(stopChan)

	<-stopChan
}

func SFlowCollector(flow chan layers.SFlowDatagram, stopChan chan struct{}) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: listenPort,
	})

	if err != nil {
		log.Fatal("Listen failed,", err)
		return
	}
	for {
		var data [100000]byte // MAX MTU
		n, addr, err := udpConn.ReadFromUDP(data[:])
		if err != nil {
			log.Printf("Read from udp server:%s failed,err:%s", addr, err)
			continue
		}
		go func() {
			raw := layers.SFlowDatagram{}
			err = raw.DecodeFromBytes(data[:n], gopacket.NilDecodeFeedback)
			if err != nil {
				log.Print(err)
				return
			}
			flow <- raw
		}()
	}
}

func ConntrackWorker(channel chan []conntrack.Flow, sFlowCache *SFlowCache, stopChan chan struct{}) {

	for {
		select {
		case flows := <-channel:
			//			fmt.Printf("%+v\n", sFlowCache.ipMac)
			flow := &v1alpha1.FlowMessage{
				AgentId: agentID,
				Flow:    []*v1alpha1.Flow{},
			}

			for _, f := range flows {
				tmp := &v1alpha1.Flow{
					OriginTuple: &v1alpha1.FlowTuple{
						Src:      f.TupleOrig.IP.SourceAddress,
						Dst:      f.TupleOrig.IP.DestinationAddress,
						EthSrc:   sFlowCache.GetMac(f.TupleOrig.IP.SourceAddress.String()),
						Protocol: uint32(f.TupleOrig.Proto.Protocol),
						SrcPort:  uint32(f.TupleOrig.Proto.SourcePort),
						DstPort:  uint32(f.TupleOrig.Proto.DestinationPort),
					},
					ReplyTuple: &v1alpha1.FlowTuple{
						Src:      f.TupleReply.IP.SourceAddress,
						Dst:      f.TupleReply.IP.DestinationAddress,
						EthSrc:   sFlowCache.GetMac(f.TupleReply.IP.SourceAddress.String()),
						Protocol: uint32(f.TupleReply.Proto.Protocol),
						SrcPort:  uint32(f.TupleReply.Proto.SourcePort),
						DstPort:  uint32(f.TupleReply.Proto.DestinationPort),
					},
					OriginCounter: &v1alpha1.FlowCounter{
						Packets: f.CountersOrig.Packets,
						Bytes:   f.CountersOrig.Bytes,
					},
					ReplyCounter: &v1alpha1.FlowCounter{
						Packets: f.CountersReply.Packets,
						Bytes:   f.CountersReply.Bytes,
					},
					OriginDir: 0,
					StartTime: uint64(f.Timestamp.Start.Unix()),
					CtId:      f.ID,
					CtTimeout: f.Timeout,
					CtZone:    uint32(f.Zone),
					CtUse:     f.Use,
					CtMark:    f.Mark,
					CtStatus:  uint32(f.Status.Value),
				}

				// skip local connections
				if f.TupleOrig.IP.SourceAddress.IsLoopback() && f.TupleOrig.IP.DestinationAddress.IsLoopback() {
					continue
				}

				if sFlowCache.GetMac(f.TupleOrig.IP.SourceAddress.String()) == nil &&
					sFlowCache.GetMac(f.TupleOrig.IP.DestinationAddress.String()) == nil {
					tmp.OriginDir = 2

				}
				if sFlowCache.GetMac(f.TupleOrig.IP.SourceAddress.String()) != nil {
					//out := fmt.Sprintf("out - %s/%d -> %s %s t:%s - %s\n", f.TupleOrig.IP.SourceAddress,
					//	sFlowCache.GetMac(f.TupleOrig.IP.SourceAddress.String()), f.TupleOrig.IP.DestinationAddress, f.CountersOrig.String(), f.Timestamp.Start.String(), f.Timestamp.Stop.String())
					tmp.OriginDir = 1
				}
				if sFlowCache.ipMac[f.TupleOrig.IP.DestinationAddress.String()] != nil {
					//in := fmt.Sprintf("in - %s -> %s/%d %s t:%s - %s\n", f.TupleOrig.IP.SourceAddress,
					//	f.TupleOrig.IP.DestinationAddress, sFlowCache.GetMac(f.TupleOrig.IP.DestinationAddress.String()), f.CountersOrig.String(), f.Timestamp.Start.String(), f.Timestamp.Stop.String())
					tmp.OriginDir = 0
				}
				flow.Flow = append(flow.Flow, tmp)
			}

			//			fmt.Println(flow)
			b, err := proto.Marshal(flow)
			if err != nil {
				log.Println(err)
			}
			ToKafka(b)

		case <-stopChan:
			return
		}
	}

}

func SFlowWorker(channel chan layers.SFlowDatagram, sFlowCache *SFlowCache, stopChan chan struct{}) {
	for {
		select {
		case flow := <-channel:
			for _, sample := range flow.FlowSamples {
				for _, record := range sample.Records {
					//fmt.Println("flow - " + reflect.TypeOf(record).Name())
					switch record.(type) {
					case layers.SFlowRawPacketFlowRecord:
						if record.(layers.SFlowRawPacketFlowRecord).Header.LinkLayer().LayerType() == layers.LayerTypeEthernet {
							packet := record.(layers.SFlowRawPacketFlowRecord).Header
							switch packet.Layers()[1].LayerType() {
							case layers.LayerTypeARP:
								arp := layers.ARP{}
								err = arp.DecodeFromBytes(packet.Layers()[1].LayerContents(), gopacket.NilDecodeFeedback)
								if err != nil || arp.AddrType != layers.LinkTypeEthernet {
									continue
								}
								// add to cache
								if sample.InputInterface != uplinkPort1 && sample.InputInterface != uplinkPort2 {
									sFlowCache.AddArp(arp)
								}
							case layers.LayerTypeIPv4:
								// add to cache
								if sample.InputInterface != uplinkPort1 && sample.InputInterface != uplinkPort2 {
									sFlowCache.AddIp(packet)
								}
							default:
								fmt.Println(packet.Layers()[1].LayerType())
							}
						}
					}
				}
			}
			for _, sample := range flow.CounterSamples {
				for _, record := range sample.Records {
					//fmt.Println("counter - " + reflect.TypeOf(record).Name())
					switch record.(type) {
					case layers.SFlowGenericInterfaceCounters:
						count := record.(layers.SFlowGenericInterfaceCounters)
						fmt.Printf("if:%d,dir %d, pkt %d\n", count.IfIndex, count.IfDirection, count.IfOutUcastPkts)
					}
				}
			}
		case <-stopChan:
			return
		}
	}
}

func ConntrackCollecter(ct chan []conntrack.Flow, stopChan chan struct{}) {

	// Open a Conntrack connection.
	c, err := conntrack.Dial(nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ticker.C:
			flows, err := c.Dump()
			if err != nil {
				log.Printf("dump flows: %s", err)
			}
			ct <- flows
		case <-stopChan:
			return
		}
	}

}

func ToKafka(msg []byte) {

	producer.Input() <- &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.ByteEncoder(uuid.NewUUID()),
		Value: sarama.ByteEncoder(msg),
	}
}

func KafkaErrorCheck(stopChan <-chan struct{}) {
	for {
		select {
		case err := <-producer.Errors():
			log.Printf("producer error: %s", err)
		case <-stopChan:
			return
		}
	}
}
