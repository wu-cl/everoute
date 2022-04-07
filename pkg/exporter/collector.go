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

package exporter

import (
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/contiv/libOpenflow/protocol"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	netlink2 "github.com/mdlayher/netlink"
	"github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/sysctl"

	"github.com/everoute/everoute/pkg/agent/datapath"
	"github.com/everoute/everoute/pkg/apis/exporter/v1alpha1"
)

const (
	ConntrackSampleInterval = 5
	TcpSocketSampleInterval = 5
	BondInfoReportInterval  = 5

	TcpTimeWaitThreshold = 10
)

type Exporter struct {
	cache    *CollectorCache
	uploader Uploader

	AgentArpChan        chan protocol.ARP
	agentArpReport      [][]byte
	agentArpReportMutex sync.Mutex

	tcpTimeWaitTimeOut uint32

	stopChan <-chan struct{}

	datapathManager *datapath.DpManager
}

func NewExporter(uploader Uploader) *Exporter {
	e := &Exporter{
		cache:    NewCollectorCache(),
		uploader: uploader,
	}
	e.AgentArpChan = make(chan protocol.ARP, 100)

	t, err := sysctl.New().GetSysctl("net/netfilter/nf_conntrack_tcp_timeout_time_wait")
	if err != nil {
		klog.Fatalf("Could not get net.netfilter.nf_conntrack_tcp_timeout_time_wait, err: %s", err)
	}
	e.tcpTimeWaitTimeOut = uint32(t)

	return e
}

func (e *Exporter) StartExporter(datapathManager *datapath.DpManager, stopChan <-chan struct{}) {
	e.datapathManager = datapathManager
	e.stopChan = stopChan

	ctChan := make(chan []conntrack.Flow, 100)
	sFlowChan := make(chan layers.SFlowDatagram, 100)

	ovsMonitor := NewMonior(e.cache)
	go ovsMonitor.Run(e.stopChan)

	if err := sysctl.New().SetSysctl("net/netfilter/nf_conntrack_acct", 1); err != nil {
		klog.Fatalf("Could not set net.netfilter.nf_conntrack_acct to 1, err: %s", err)
	}
	if err := sysctl.New().SetSysctl("net/netfilter/nf_conntrack_timestamp", 1); err != nil {
		klog.Fatalf("Could not set net.netfilter.nf_conntrack_timestamp to 1, err: %s", err)
	}

	go e.conntractCollector(ctChan)
	go e.conntrackWorker(ctChan, e.uploader)

	go e.sFlowCollector(sFlowChan)
	go e.sFlowWorker(sFlowChan)

	go e.tcpSocketCollector()

	go e.agentArpProcess()

	go e.bondInfoProcess()

	<-e.stopChan
}

func (e *Exporter) bondInfoProcess() {
	ticker := time.NewTicker(time.Second * BondInfoReportInterval)
	for {
		select {
		case <-ticker.C:
			bondMsg := OvsBondInfo()
			e.uploader.Bond(bondMsg)
		case <-e.stopChan:
			return
		}
	}
}

func (e *Exporter) tcpSocketCollector() {
	ticker := time.NewTicker(time.Second * TcpSocketSampleInterval)
	for {
		select {
		case <-ticker.C:
			info, err := netlink.SocketDiagTCPInfo(netlink.FAMILY_V4)
			if err != nil {
				klog.Errorf("fail to get tcp socket info %s", err)
			}
			for _, item := range info {
				if item.TCPInfo != nil && item.InetDiagMsg != nil {
					// filter loopback
					if item.InetDiagMsg.ID.Source.IsLoopback() || item.InetDiagMsg.ID.Destination.IsLoopback() {
						continue
					}
					// filter local
					if item.InetDiagMsg.ID.Source.Equal(item.InetDiagMsg.ID.Destination) {
						continue
					}
					_ = e.cache.tcpSocketCache.Add(&TcpSocket{
						localAddr: item.InetDiagMsg.ID.Source.String(),
						localPort: item.InetDiagMsg.ID.SourcePort,
						peerAddr:  item.InetDiagMsg.ID.Destination.String(),
						peerPort:  item.InetDiagMsg.ID.DestinationPort,
						state:     item.TCPInfo.State,
						caState:   item.TCPInfo.Ca_state,
						rto:       item.TCPInfo.Rto,
						rtt:       item.TCPInfo.Rtt,
						rttVar:    item.TCPInfo.Rttvar,
					})
					//klog.Infof("tcp %s:%d rto: %d", item.InetDiagMsg.ID.Source.String(), item.InetDiagMsg.ID.SourcePort, item.TCPInfo.Rto)
				}
			}

		case <-e.stopChan:
			return
		}
	}
}

func (e *Exporter) agentArpProcess() {
	for {
		select {
		case arp := <-e.AgentArpChan:
			b, err := arp.MarshalBinary()
			if err != nil {
				continue
			}
			arpPkt := layers.ARP{}
			err = arpPkt.DecodeFromBytes(b, gopacket.NilDecodeFeedback)
			if err != nil {
				continue
			}
			e.cache.AddArp(arpPkt)

			e.agentArpReportMutex.Lock()
			e.agentArpReport = append(e.agentArpReport, b)
			e.agentArpReportMutex.Unlock()
		case <-e.stopChan:
			return
		}
	}
}

func (e *Exporter) sFlowCollector(flow chan layers.SFlowDatagram) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: SFlowPort,
	})

	if err != nil {
		klog.Fatal("Listen failed,", err)
		return
	}
	for {
		var data [100000]byte // MAX MTU
		n, addr, err := udpConn.ReadFromUDP(data[:])
		if err != nil {
			klog.Errorf("Read from udp server:%s failed,err:%s", addr, err)
			continue
		}
		go func() {
			raw := layers.SFlowDatagram{}
			err = raw.DecodeFromBytes(data[:n], gopacket.NilDecodeFeedback)
			if err != nil {
				klog.Errorf("decode sflow datagram error, err:%s", err)
				return
			}
			flow <- raw
		}()
	}
}

func (e *Exporter) ctFilter(ct conntrack.Flow) bool {
	// skip loopBack connections
	if ct.TupleOrig.IP.SourceAddress.IsLoopback() && ct.TupleOrig.IP.DestinationAddress.IsLoopback() {
		return true
	}

	// skip time_wait tcp flow
	if ct.TupleOrig.Proto.Protocol == uint8(layers.IPProtocolTCP) {
		if ct.ProtoInfo.TCP != nil && ct.ProtoInfo.TCP.State == TcpConntrackTimeWait {
			if ct.Timeout < e.tcpTimeWaitTimeOut-TcpTimeWaitThreshold {
				return true
			}
		}
	}

	// skip local connections
	if ct.TupleOrig.IP.SourceAddress.String() == ct.TupleOrig.IP.DestinationAddress.String() &&
		ct.TupleReply.IP.SourceAddress.String() == ct.TupleReply.IP.DestinationAddress.String() &&
		ct.TupleOrig.IP.SourceAddress.String() == ct.TupleReply.IP.DestinationAddress.String() {
		return true
	}

	return false
}

func (e *Exporter) ctItemToFlow(ct conntrack.Flow) *v1alpha1.Flow {
	flow := &v1alpha1.Flow{
		Protocol: uint32(ct.TupleOrig.Proto.Protocol),
		OriginTuple: &v1alpha1.FlowTuple{
			Src:     ct.TupleOrig.IP.SourceAddress,
			Dst:     ct.TupleOrig.IP.DestinationAddress,
			EthSrc:  e.cache.GetMac(ct.TupleOrig.IP.SourceAddress.String()),
			SrcPort: uint32(ct.TupleOrig.Proto.SourcePort),
			DstPort: uint32(ct.TupleOrig.Proto.DestinationPort),
		},
		ReplyTuple: &v1alpha1.FlowTuple{
			Src:     ct.TupleReply.IP.SourceAddress,
			Dst:     ct.TupleReply.IP.DestinationAddress,
			EthSrc:  e.cache.GetMac(ct.TupleReply.IP.SourceAddress.String()),
			SrcPort: uint32(ct.TupleReply.Proto.SourcePort),
			DstPort: uint32(ct.TupleReply.Proto.DestinationPort),
		},
		OriginCounter: &v1alpha1.FlowCounter{
			Packets: ct.CountersOrig.Packets,
			Bytes:   ct.CountersOrig.Bytes,
		},
		ReplyCounter: &v1alpha1.FlowCounter{
			Packets: ct.CountersReply.Packets,
			Bytes:   ct.CountersReply.Bytes,
		},
		StartTime:  uint64(ct.Timestamp.Start.Unix()),
		UpdateTime: uint64(time.Now().Unix()),
		CtId:       ct.ID,
		CtTimeout:  ct.Timeout,
		CtZone:     uint32(ct.Zone),
		CtUse:      ct.Use,
		CtMark:     ct.Mark,
		CtStatus:   uint32(ct.Status.Value),
		CtLabel:    ct.Labels,
	}
	// calculate ct direction
	// TODO: const direction and align with policy rule direction
	if e.cache.GetMac(ct.TupleOrig.IP.SourceAddress.String()) == nil &&
		e.cache.GetMac(ct.TupleOrig.IP.DestinationAddress.String()) == nil {
		flow.OriginDir = v1alpha1.DIR_CT_ORIGIN_UNKNOWN
	}
	if e.cache.GetMac(ct.TupleOrig.IP.SourceAddress.String()) != nil {
		flow.OriginDir = v1alpha1.DIR_CT_ORIGIN_OUT
	}
	if e.cache.GetMac(ct.TupleOrig.IP.DestinationAddress.String()) != nil {
		flow.OriginDir = v1alpha1.DIR_CT_ORIGIN_IN
	}
	if e.cache.GetMac(ct.TupleOrig.IP.SourceAddress.String()) != nil &&
		e.cache.GetMac(ct.TupleOrig.IP.DestinationAddress.String()) != nil {
		flow.OriginDir = v1alpha1.DIR_CT_ORIGIN_LOCAL
	}

	// fetch uplink interface name, empty if dir is known
	switch flow.OriginDir {
	case v1alpha1.DIR_CT_ORIGIN_IN:
		flow.BondUplinkIfname = e.cache.FetchIpBondInterface(ct.TupleOrig.IP.DestinationAddress.String())
	case v1alpha1.DIR_CT_ORIGIN_OUT:
		flow.BondUplinkIfname = e.cache.FetchIpBondInterface(ct.TupleOrig.IP.SourceAddress.String())
	}

	// fetch socket info into flow
	if flow.Protocol == uint32(layers.IPProtocolTCP) && ct.ProtoInfo.TCP != nil {
		flow.ProtocolInfo = &v1alpha1.ProtocolInfo{
			TcpInfo: &v1alpha1.TcpInfo{
				State: uint32(TcpCTStatusToSocketStatus(ct.ProtoInfo.TCP.State)),
			},
		}

		tcpSocket := e.cache.FetchSocketByFlow(flow)
		if tcpSocket != nil {
			flow.ProtocolInfo.TcpInfo.State = uint32(tcpSocket.state)
			flow.ProtocolInfo.TcpInfo.CaState = uint32(tcpSocket.caState)
			flow.ProtocolInfo.TcpInfo.Rto = tcpSocket.rto
			flow.ProtocolInfo.TcpInfo.Rtt = tcpSocket.rtt
			flow.ProtocolInfo.TcpInfo.RttVar = tcpSocket.rttVar
		}
	}

	// fetch policy info into flow
	if len(flow.CtLabel) != 0 {
		flowID1, flowID2, flowID3 := e.ctLabelDecode(ct.Labels)
		policyList := e.datapathManager.GetPolicyByFlowID(flowID1, flowID2, flowID3)
		for _, policySet := range policyList {
			for _, policyItem := range policySet.Item {
				flow.Policy = append(flow.Policy, &v1alpha1.Policy{
					Name:      policyItem.Name,
					Namespace: policyItem.Namespace,
					Type:      string(policyItem.PolicyType),
					Dir:       uint32(policySet.Dir),
					Action:    policySet.Action,
					Mode:      policySet.Mode,
				})
			}
		}
	}

	return flow
}

func (e *Exporter) conntrackWorker(channel chan []conntrack.Flow, uploader Uploader) {
	for {
		select {
		case flows := <-channel:
			flow := &v1alpha1.FlowMessage{
				Flow: []*v1alpha1.Flow{},
			}
			for _, f := range flows {
				// filter un-ness flow
				if e.ctFilter(f) {
					continue
				}
				flow.Flow = append(flow.Flow, e.ctItemToFlow(f))
			}
			uploader.Flow(flow)
		case <-e.stopChan:
			return
		}
	}

}

func (e *Exporter) sFlowWorker(channel chan layers.SFlowDatagram) {
	for {
		select {
		case flow := <-channel:
			// handle flow sample packet
			pktMsg := &v1alpha1.PktMessage{}
			arpCounter := map[uint32]*v1alpha1.ArpCounter{}
			for _, sample := range flow.FlowSamples {
				//klog.Infof("%#X/%#X -> %#X/%#X", sample.InputInterface, sample.InputInterfaceFormat, sample.OutputInterface, sample.InputInterfaceFormat)
				pktMsg.SampleRate = sample.SamplingRate
				pktMsg.SamplePool = sample.SamplePool
				pktMsg.Dropped += sample.Dropped
				for _, record := range sample.Records {
					switch record.(type) {
					case layers.SFlowRawPacketFlowRecord:
						if record.(layers.SFlowRawPacketFlowRecord).Header.LinkLayer().LayerType() == layers.LayerTypeEthernet {
							packet := record.(layers.SFlowRawPacketFlowRecord).Header
							switch packet.Layers()[1].LayerType() {
							case layers.LayerTypeARP:
								arp := layers.ARP{}
								err := arp.DecodeFromBytes(packet.Layers()[1].LayerContents(), gopacket.NilDecodeFeedback)
								if err != nil || arp.AddrType != layers.LinkTypeEthernet {
									continue
								}

								// calculate arp counter
								if _, ok := arpCounter[sample.InputInterface]; !ok {
									arpCounter[sample.InputInterface] = &v1alpha1.ArpCounter{}
								}
								if _, ok := arpCounter[sample.OutputInterface]; !ok {
									arpCounter[sample.OutputInterface] = &v1alpha1.ArpCounter{}
								}
								switch arp.AddrType {
								case layers.ARPRequest:
									arpCounter[sample.InputInterface].InArpRequest += sample.SamplingRate
									arpCounter[sample.OutputInterface].OutArpRequest += sample.SamplingRate
								case layers.ARPReply:
									arpCounter[sample.InputInterface].InArpReply += sample.SamplingRate
									arpCounter[sample.OutputInterface].OutArpReply += sample.SamplingRate
								}

								// only cache arp ARPRequest packet
								if arp.AddrType == layers.ARPReply {
									continue
								}
								// add to cache
								if e.cache.IsLocalIface(sample.InputInterface) {
									e.cache.AddArp(arp)
								}
								pktMsg.RawArp = append(pktMsg.RawArp, packet.Data())
							case layers.LayerTypeIPv4:
								if e.cache.IsLocalIface(sample.InputInterface) {
									e.cache.AddIp(packet)
								}
								e.cache.CheckAndAddBondIp(sample.InputInterface, packet)
								pktMsg.RawIp = append(pktMsg.RawIp, packet.Data())
							}
						}
					}
				}
			}
			// add agent arp
			e.agentArpReportMutex.Lock()
			pktMsg.RawArp = append(pktMsg.RawArp, e.agentArpReport...)
			e.agentArpReport = nil
			e.agentArpReportMutex.Unlock()

			// report arp
			if len(pktMsg.RawArp) != 0 || len(pktMsg.RawIp) != 0 {
				e.uploader.SFlowSample(pktMsg)
			}

			// handle counter sample flow
			counterMsg := &v1alpha1.CounterMessage{}
			for _, sample := range flow.CounterSamples {
				for _, record := range sample.Records {
					switch record.(type) {
					case layers.SFlowGenericInterfaceCounters:
						item := record.(layers.SFlowGenericInterfaceCounters)
						counter, exist, err := e.cache.sFlowCounterCache.Get(&SflowCounter{ifindex: item.IfIndex})
						if !exist || err != nil {
							e.cache.AddSFlowCounter(item)
							continue
						}
						counterLast := counter.(*SflowCounter)
						counterMsg.Counter = append(counterMsg.Counter, &v1alpha1.Counter{
							Ifname:           e.cache.GetIfName(item.IfIndex),
							ExternalId:       e.cache.GetIfExternalID(item.IfIndex),
							Type:             item.IfType,
							LinkSpeed:        item.IfSpeed,
							Direction:        item.IfDirection,
							Status:           item.IfStatus,
							ArpCounter:       arpCounter[item.IfIndex],
							InOctets:         item.IfInOctets - counterLast.InOctets,
							InUcastPkts:      item.IfInUcastPkts - counterLast.InUcastPkts,
							InMulticastPkts:  item.IfInMulticastPkts - counterLast.InMulticastPkts,
							InBroadcastPkts:  item.IfInBroadcastPkts - counterLast.InBroadcastPkts,
							InDiscards:       item.IfInDiscards - counterLast.InDiscards,
							InErrors:         item.IfInErrors - counterLast.InErrors,
							InUnknownProtos:  item.IfInUnknownProtos - counterLast.InUnknownProtos,
							OutOctets:        item.IfOutOctets - counterLast.OutOctets,
							OutUcastPkts:     item.IfOutUcastPkts - counterLast.OutUcastPkts,
							OutMulticastPkts: item.IfOutMulticastPkts - counterLast.OutMulticastPkts,
							OutBroadcastPkts: item.IfOutBroadcastPkts - counterLast.OutBroadcastPkts,
							OutDiscards:      item.IfOutDiscards - counterLast.OutDiscards,
							OutErrors:        item.IfOutErrors - counterLast.OutErrors,
							PromiscuousMode:  item.IfPromiscuousMode,
						})
						e.cache.AddSFlowCounter(item)
					}
				}
			}
			if len(counterMsg.Counter) != 0 {
				e.uploader.SFlowCounter(counterMsg)
			}
		case <-e.stopChan:
			return
		}
	}
}

func (e *Exporter) ctEventHandle(c chan conntrack.Event) {
	for {
		select {
		case event := <-c:
			if event.Type == conntrack.EventDestroy && !e.ctFilter(*event.Flow) {
				flow := e.ctItemToFlow(*event.Flow)
				flowMsg := &v1alpha1.FlowMessage{
					Flow: []*v1alpha1.Flow{flow},
				}
				e.uploader.Flow(flowMsg)
			}
		case <-e.stopChan:
			return
		}
	}
}

func (e *Exporter) ctLabelDecode(label []byte) (uint64, uint64, uint64) {
	// Bit Order Example:
	//
	// No.1 1010 0010 1111 0001 0xA2F1  -  ovs register order
	// No.2 1000 1111 0100 0101 0x8F45  -  ovs dpctl/dump-conntrack (left-right mirror from No.1)
	// No.3 0100 0101 1000 1111 0x458F  -  netlink ct label
	//
	// label retrieve from netlink ct label, transfer it with little endian
	// In the above case, it seems as No.2
	// Since binary lib could only handle uint64, label (128 bits) split into TWO parts.
	//
	// The round number stores in high 10 bits. Here it means the right 10 bits in uint64 partA.

	partA := binary.LittleEndian.Uint64(label[0:8])
	partB := binary.LittleEndian.Uint64(label[8:16])

	var RoundMask uint64 = 0x0000_0000_0000_03FF
	var flowSeq1Mask uint64 = 0x0000_0000_FFFF_FC00
	var flowSeq2Mask uint64 = 0x003F_FFFF_0000_0000
	var flowSeq3MaskPartA uint64 = 0xFFC0_0000_0000_0000
	var flowSeq3MaskPartB uint64 = 0x0000_0000_0000_0FFF

	roundNum := (partA & RoundMask) << 22
	flowSeq1 := (partA & flowSeq1Mask) >> 10
	flowSeq2 := (partA & flowSeq2Mask) >> (10 + 22)
	flowSeq3 := ((partA & flowSeq3MaskPartA) >> (10 + 22 + 22)) | ((partB & flowSeq3MaskPartB) << 10)

	var flowID1, flowID2, flowID3 uint64
	if flowSeq1 != 0 {
		flowID1 = roundNum | flowSeq1
	}
	if flowSeq2 != 0 {
		flowID2 = roundNum | flowSeq2
	}
	if flowSeq3 != 0 {
		flowID3 = roundNum | flowSeq3
	}
	return flowID1, flowID2, flowID3
}

func (e *Exporter) conntractCollector(ct chan []conntrack.Flow) {
	// open conntrack connection
	eventConn, err := conntrack.Dial(nil)
	if err != nil {
		klog.Fatal(err)
	}
	defer eventConn.Close()
	dumpConn, err := conntrack.Dial(nil)
	if err != nil {
		klog.Fatal(err)
	}
	defer dumpConn.Close()

	// add event handle
	eventCh := make(chan conntrack.Event, 1024)
	_, err = eventConn.Listen(eventCh, 1, append(netfilter.GroupsCT))
	if err != nil {
		klog.Fatal(err)
	}
	err = eventConn.SetOption(netlink2.ListenAllNSID, true)
	if err != nil {
		klog.Fatal(err)
	}
	go e.ctEventHandle(eventCh)

	// dump all ct flow periodically
	ticker := time.NewTicker(time.Second * ConntrackSampleInterval)
	for {
		select {
		case <-ticker.C:
			flows, err := dumpConn.Dump()
			if err != nil {
				klog.Errorf("dump flows: %s", err)
			}
			ct <- flows
		case <-e.stopChan:
			return
		}
	}
}