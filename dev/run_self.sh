#!/bin/bash
filename=`date +%s`.log

cd /root/everoute

git checkout .
git pull origin main

skaffold run -d=harbor.smartx.com/everoute --status-check=true

commitid=`git log -n 1 | head -n 1 | awk '{print $2}'`

echo "start e2e test"
cd /root/script
/root/kubernetes/_output/bin/ginkgo $1 -noColor -nodes=20 --skip="named port|SCTP" --focus="NetworkPolicy" /root/kubernetes/_output/bin/e2e.test -- --disable-log-dump --provider="skeleton" --kubeconfig="/root/.kube/config" > report/$filename

success=`cat /root/script/report/$filename | tail -n 5 | head -n 1 | awk '{print $3}'`
fail=`cat /root/script/report/$filename | tail -n 5 | head -n 1 | awk '{print $6}'`
usetime=`cat /root/script/report/$filename | tail -n 2 | head -n 1 | awk '{print $NF}'`

color="DF0000"
emoji=":x:"
if [ "$fail" == "0" ]; then
    color="2EA44F"
    emoji=":white_check_mark:"
fi

curl -X POST -H 'Content-type: application/json' https://hooks.slack.com/services/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  --data '{"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Finish Everoute K8S E2E Test, logs <http://192.168.28.7/'$filename' | `here`> | main"}}],"attachments":[{"color":"'$color'","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*Commit: * <https://github.com/everoute/everoute/commit/'$commitid'|`'$commitid'`>"}},{"type":"section","text":{"type":"mrkdwn","text":"*Result: * *'$success'* Passed  |  *'$fail'* Failed '$emoji'"}},{"type":"section","text":{"type":"mrkdwn","text":"*Usetime: * '$usetime'"}}]}]}'