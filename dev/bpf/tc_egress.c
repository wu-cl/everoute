// tc-xdp-drop-tcp.c
#include <stdbool.h>
#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/in.h>
#include <linux/pkt_cls.h>

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

SEC("tc")
int tc_redirect_skb(struct __sk_buff *skb)
{
  void *data = (void *)(long)skb->data;
  void *data_end = (void *)(long)skb->data_end;

  struct ethhdr *eth = data;
  if (data + sizeof(*eth) > data_end){
    return TC_ACT_OK;
  }

  if (eth->h_proto == bpf_htons(ETH_P_IP))
  {
    struct iphdr *iph = (struct iphdr *)(eth + 1);
    if ((void *)(iph + 1) > data_end)
        return TC_ACT_OK;
    unsigned int ip_src = iph->saddr;
    unsigned int ip_dst = iph->daddr;
    bpf_printk("egress src ip addr ifindex %d : %d.%d\n",skb->ingress_ifindex,(ip_src >> 16) & 0xFF, (ip_src >> 24) & 0xFF);

    bpf_printk("egress dest ip addr: %d.%d\n",(ip_dst >> 16) & 0xFF,(ip_dst >> 24) & 0xFF);
  }

  //bpf_redirect(7,BPF_F_INGRESS);


  return TC_ACT_OK;

}

char _license[] SEC("license") = "GPL";