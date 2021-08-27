package proxy

import (
	"context"
	"fmt"
	"github.com/coreos/go-iptables/iptables"
	lynxctrl "github.com/smartxworks/lynx/pkg/controller"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"net"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
	"sync"
	"time"
)

// NodeReconciler watch node and update route table
type NodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	StopChan    <-chan struct{}
	updateMutex sync.Mutex
}

func GetNodeInternalIP(node corev1.Node) net.IP {
	for _, item := range node.Status.Addresses {
		if item.Type == corev1.NodeInternalIP {
			return net.ParseIP(item.Address)
		}
	}
	return nil
}

func GetRouteByDst(dst *net.IPNet) []netlink.Route {
	var ret []netlink.Route
	// List all route item in current node
	routeList, err := netlink.RouteList(nil, unix.AF_INET)
	if err != nil {
		klog.Errorf("List route table error, err:%s")
		return ret
	}
	for _, item := range routeList {
		if item.Dst != nil && item.Dst.String() == dst.String() {
			ret = append(ret, item)
		}
	}
	return ret
}
func RouteEqual(r1, r2 netlink.Route) bool {
	return ((r1.Dst == nil && r2.Dst == nil) ||
		(r1.Dst != nil && r2.Dst != nil && r1.Dst.String() == r2.Dst.String())) &&
		r1.Src.Equal(r2.Src) &&
		r1.Gw.Equal(r2.Gw)
}

func UpdateRoute(nodeList corev1.NodeList, thisNode corev1.Node) {
	var oldRoute []netlink.Route
	var newRoute []netlink.Route

	for _, item := range nodeList.Items {
		// ignore current node
		if item.Name == thisNode.Name {
			continue
		}
		gw := GetNodeInternalIP(item)
		if gw == nil {
			klog.Errorf("Fail to get node internal IP in node: %s", item.Name)
			continue
		}
		// multi-podCIDRs will create multi-routeItem
		for _, podCIDR := range item.Spec.PodCIDRs {
			dst, err := netlink.ParseIPNet(podCIDR)
			if err != nil {
				klog.Errorf("Parse podCIDR %s failed, err: %s", podCIDR, err)
			}
			tempRoute := GetRouteByDst(dst)
			if len(tempRoute) != 0 {
				oldRoute = append(oldRoute, tempRoute...)
			}
			newRoute = append(newRoute, netlink.Route{
				Dst:   dst,
				Gw:    gw,
				Table: 254,
			})
		}
	}

	var delRoute []netlink.Route
	var addRoute []netlink.Route
	// calculate route to delete
	for _, oldItem := range oldRoute {
		exist := false
		for _, newItem := range newRoute {
			if RouteEqual(oldItem, newItem) {
				exist = true
				break
			}
		}
		if !exist {
			delRoute = append(delRoute, oldItem)
		}
	}
	// calculate route to add
	for _, newItem := range newRoute {
		exist := false
		for _, oldItem := range oldRoute {
			if RouteEqual(oldItem, newItem) {
				exist = true
				break
			}
		}
		if !exist {
			addRoute = append(delRoute, newItem)
		}
	}

	// add & del route
	for _, item := range delRoute {
		if err := netlink.RouteDel(&item); err != nil {
			klog.Errorf("delete route item failed, err: %s", err)
		} else {
			klog.Infof("delete route item %s", item)
		}
	}
	for _, item := range addRoute {
		if err := netlink.RouteAdd(&item); err != nil {
			klog.Errorf("add route item failed, err: %s", err)
		} else {
			klog.Infof("add route item %s", item)
		}
	}
}

func UpdateIptables(nodeList corev1.NodeList, thisNode corev1.Node) {
	var exist bool

	ipt, err := iptables.New()
	if err != nil {
		klog.Errorf("init iptables error, err: %s", err)
		return
	}

	// check existence of chain LYNX-OUTPUT, if not, then create it
	if exist, err = ipt.ChainExists("nat", "LYNX-OUTPUT"); err != nil {
		klog.Errorf("Get iptables LYNX-OUTPUT error, error: %s", err)
		return
	}
	if !exist {
		err = ipt.NewChain("nat", "LYNX-OUTPUT")
		if err != nil {
			klog.Errorf("Create iptables LYNX-OUTPUT error, error: %s", err)
			return
		}
	}

	// check and add LYNX-OUTPUT to POSTROUTING
	if exist, err = ipt.Exists("nat", "POSTROUTING", "-j", "LYNX-OUTPUT"); err != nil {
		klog.Error("Check LYNX-OUTPUT in nat POSTROUTING error, err: %s", err)
	}
	if !exist {
		if err = ipt.Append("nat", "POSTROUTING", "-j", "LYNX-OUTPUT"); err != nil {
			klog.Error("Append LYNX-OUTPUT into nat POSTROUTING error, err: %s", err)
		}
	}

	// check and add MASQUERADE in LYNX-OUTPUT"
	for _, podCIDR := range thisNode.Spec.PodCIDRs {
		ruleSpec := []string{"-s", podCIDR, "-j", "MASQUERADE"}
		if exist, err = ipt.Exists("nat", "LYNX-OUTPUT", ruleSpec...); err != nil {
			klog.Error("Check MASQUERADE rule in nat LYNX-OUTPUT error, rule: %s, err: %s", ruleSpec, err)
			continue
		}
		if !exist {
			err = ipt.Append("nat", "LYNX-OUTPUT", ruleSpec...)
			if err != nil {
				klog.Error("Add MASQUERADE rule in nat LYNX-OUTPUT error, rule: %s, err: %s", ruleSpec, err)
				continue
			}
		}
	}

	// check and add ACCEPT in LYNX-OUTPUT"
	for _, podCIDR := range thisNode.Spec.PodCIDRs {
		for _, nodeItem := range nodeList.Items {
			if nodeItem.Name == thisNode.Name {
				continue
			}
			for _, otherPodCIDR := range nodeItem.Spec.PodCIDRs {
				ruleSpec := []string{"-s", podCIDR, "-d", otherPodCIDR, "-j", "ACCEPT"}
				if exist, err = ipt.Exists("nat", "LYNX-OUTPUT", ruleSpec...); err != nil {
					klog.Error("Check ACCEPT rule in nat LYNX-OUTPUT error, rule: %s, err: %s", ruleSpec, err)
					continue
				}
				if !exist {
					if err = ipt.Insert("nat", "LYNX-OUTPUT", 1, ruleSpec...); err != nil {
						klog.Error("Add ACCEPT rule in nat LYNX-OUTPUT error, rule: %s, err: %s", ruleSpec, err)
						continue
					}
				}
			}
		}
	}
}

func (r *NodeReconciler) UpdateNetwork() {
	klog.Infof("update network config")
	r.updateMutex.Lock()
	defer r.updateMutex.Unlock()

	// List all nodes in cluster
	nodeList := corev1.NodeList{}
	if err := r.List(context.Background(), &nodeList); err != nil {
		klog.Errorf("List Node error, err: %s", err)
		return
	}
	// Get current node
	nodeName, _ := os.Hostname()
	nodeName = strings.ToLower(nodeName)
	var currentNode corev1.Node
	for _, item := range nodeList.Items {
		if item.Name == nodeName {
			currentNode = item
			break
		}
	}

	UpdateRoute(nodeList, currentNode)
	UpdateIptables(nodeList, currentNode)
}

// Reconcile receive endpoint from work queue, synchronize the endpoint status
// from agentinfo.
func (r *NodeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("NodeReconciler received node %s reconcile", req.NamespacedName)

	r.UpdateNetwork()

	return ctrl.Result{}, nil
}

// SetupWithManager create and add Endpoint Controller to the manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if mgr == nil {
		return fmt.Errorf("can't setup with nil manager")
	}

	c, err := controller.New("node-controller", mgr, controller.Options{
		MaxConcurrentReconciles: lynxctrl.DefaultMaxConcurrentReconciles,
		Reconciler:              r,
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	ticker := time.NewTicker(100 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				r.UpdateNetwork()
			case <-r.StopChan:
				return
			}
		}
	}()

	return nil
}
