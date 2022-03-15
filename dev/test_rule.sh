#!/bin/bash


i=1
times=$1
while [ $i -le $times ]
do



  psd="/proc/sys/kernel/random/uuid"
  echo $(cat $psd)
  UUID=$(cat /proc/sys/kernel/random/uuid)

cat > "rule/rule-$UUID.yaml" << EOF
apiVersion: policyrule.everoute.io/v1alpha1
kind: PolicyRule
metadata:
  name: test-$UUID
spec:
  action: Allow
  direction: Ingress
  dstIPAddr: 192.168.23.143/32
  dstPort: $i
  ipProtocol: ""
  ruleType: NormalRule
  tcpFlags: ""
  tier: tier1
EOF

  let "i++"
done