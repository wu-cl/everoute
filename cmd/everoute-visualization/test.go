package main

import (
	"bytes"
	"fmt"
	"github.com/everoute/everoute/pkg/apis/exporter/v1alpha1"
	"io/ioutil"
	"k8s.io/klog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const vSwitchdPath = "/var/run/openvswitch/ovs-vswitchd"

func main() {

	klog.Info(OvsBondInfo())
}

func getOvsPid() string {
	data, err := ioutil.ReadFile(vSwitchdPath + ".pid")
	if err != nil {
		return "*"
	}
	strconv.ParseUint()
	return strings.Trim(string(data), " \n")
}

func OvsAppCtl(args ...string) []string {
	cmd := fmt.Sprintf("ovs-appctl -t %s.%s.ctl ", vSwitchdPath, getOvsPid())
	for _, item := range args {
		cmd += item + " "
	}

	b, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	if err != nil {
		klog.Errorf("exec ovs-appctl error, cmd:%s, err:%s", cmd, err)
		return nil
	}
	out := bytes.NewBuffer(b).String()

	klog.Info(out)

	return strings.Split(strings.TrimSpace(out), "\n")
}

func parseTimeDuration(duration string) uint64 {
	d, err := time.ParseDuration(strings.ReplaceAll(duration, " ", ""))
	if err != nil {
		klog.Infof("parse time duration error,err:%s", err)
		return 0
	}
	return uint64(d.Milliseconds())
}

func strToUint64(str string) uint64 {
	num, _ := strconv.Atoi(str)
	return uint64(num)
}

func OvsBondInfo() *v1alpha1.BondMsg {
	msg := &v1alpha1.BondMsg{
		Ports: make(map[string]*v1alpha1.BondPort),
	}
	// bond/show process
	bondInfo := OvsAppCtl("bond/show")
	var portName string
	var ifaceName string
	var isBasicInfo = true
	for _, line := range bondInfo {
		if strings.HasPrefix(line, "----") {
			portName = strings.Trim(line, "- ")
			msg.Ports[portName] = &v1alpha1.BondPort{
				BondName: portName,
				Ifs:      make(map[string]*v1alpha1.BondInterface),
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			isBasicInfo = false
			continue
		}
		if isBasicInfo {
			key := strings.TrimSpace(strings.Split(line, ":")[0])
			value := strings.TrimSpace(strings.Join(strings.Split(line, ":")[1:], ":"))
			switch key {
			case "bond_mode":
				msg.Ports[portName].BondMode = value
			case "updelay":
				msg.Ports[portName].UpDelay = parseTimeDuration(value)
			case "downdelay":
				msg.Ports[portName].DownDelay = parseTimeDuration(value)
			case "next rebalance":
				msg.Ports[portName].NextRebalance = parseTimeDuration(value)
			case "lacp_status":
				msg.Ports[portName].LacpStatus = value
			case "lacp_fallback_ab":
				msg.Ports[portName].LacpFallbackAb = value
			case "active slave mac":
				mac, _ := net.ParseMAC(strings.Split(value, "(")[0])
				msg.Ports[portName].ActiveSlaveMac = mac
				msg.Ports[portName].ActiveSlaveInterfaceName = strings.Trim(strings.Split(value, "(")[1], ")")
			}
		} else {
			if strings.HasPrefix(line, "slave") {
				ifaceName = strings.TrimSpace(strings.TrimPrefix(strings.Split(line, ":")[0], "slave"))
				if msg.Ports[portName].Ifs[ifaceName] == nil {
					msg.Ports[portName].Ifs[ifaceName] = &v1alpha1.BondInterface{}
				}
				msg.Ports[portName].Ifs[ifaceName].Name = ifaceName
				msg.Ports[portName].Ifs[ifaceName].Status = strings.TrimSpace(strings.Split(line, ":")[1])
			}
			if strings.TrimSpace(line) == "active slave" {
				msg.Ports[portName].Ifs[ifaceName].IsActiveSlave = true
			}
			if strings.HasPrefix(strings.TrimSpace(line), "may_enable") {
				mayEnable, _ := strconv.ParseBool(strings.TrimSpace(strings.Split(line, ":")[1]))
				msg.Ports[portName].Ifs[ifaceName].MayEnable = mayEnable
			}
		}
	}

	// lacp/show-stats process
	lacpInfo := OvsAppCtl("lacp/show-stats")
	for _, line := range lacpInfo {
		if strings.HasPrefix(line, "----") {
			portName = strings.TrimSpace(strings.ReplaceAll(strings.Trim(line, "- "), "statistics", ""))
			continue
		}
		if msg.Ports[portName] == nil {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "slave") {
			ifaceName = strings.TrimSpace(strings.Split(line, ":")[1])
			continue
		}
		if msg.Ports[portName].Ifs[ifaceName] == nil {
			continue
		}
		key := strings.TrimSpace(strings.Split(line, ":")[0])
		value := strings.TrimSpace(strings.Join(strings.Split(line, ":")[1:], ":"))
		switch key {
		case "TX PDUs":
			msg.Ports[portName].Ifs[ifaceName].TxPdu = strToUint64(value)
		case "RX PDUs":
			msg.Ports[portName].Ifs[ifaceName].RxPdu = strToUint64(value)
		case "RX Bad PDUs":
			msg.Ports[portName].Ifs[ifaceName].RxBadPdu = strToUint64(value)
		case "RX Marker Request PDUs":
			msg.Ports[portName].Ifs[ifaceName].RxMarkerRequestPdu = strToUint64(value)
		case "Link Expired":
			msg.Ports[portName].Ifs[ifaceName].LinkExpired = strToUint64(value)
		case "Link Defaulted":
			msg.Ports[portName].Ifs[ifaceName].LinkDefaulted = strToUint64(value)
		case "Carrier Status Changed":
			msg.Ports[portName].Ifs[ifaceName].CarrierStatusChange = strToUint64(value)
		}
	}
	return msg
}
