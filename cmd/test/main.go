package main

import (
	"encoding/binary"
	"os"

	"github.com/ti-mo/conntrack"
	"k8s.io/klog"
)

func main() {
	dumpConn, err := conntrack.Dial(nil)
	defer dumpConn.Close()
	if err != nil {
		klog.Fatal(err)
	}

	flows, _ := dumpConn.Dump()
	for _, flow := range flows {
		if flow.TupleOrig.IP.SourceAddress.String() == os.Args[1] && flow.TupleOrig.IP.DestinationAddress.String() == os.Args[2] {
			if flow.Zone == 0 {
				continue
			}
			klog.Infoln(CTLabelDecode(flow.Labels))
		}
	}

}

func CTLabelDecode(label []byte) (uint64, uint64, uint64) {
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

	var RoundMask uint64 = 0x0000_0000_0000_000F
	var flowSeq1Mask uint64 = 0x0000_0000_FFFF_FFF0
	var flowSeq2Mask uint64 = 0x0FFF_FFFF_0000_0000
	var flowSeq3MaskPartA uint64 = 0xF000_0000_0000_0000
	var flowSeq3MaskPartB uint64 = 0x0000_0000_00FF_FFFF

	roundNum := (partA & RoundMask) << 28
	flowSeq1 := (partA & flowSeq1Mask) >> 4
	flowSeq2 := (partA & flowSeq2Mask) >> (4 + 28)
	flowSeq3 := ((partA & flowSeq3MaskPartA) >> (4 + 28 + 28)) | ((partB & flowSeq3MaskPartB) << 4)

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
