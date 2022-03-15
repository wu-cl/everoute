#!/bin/bash
filename=`date +%s`.log

cd /root/everoute
bash dev/make-dev.sh
sleep 5

commitid=`git log -n 1 | head -n 1 | awk '{print $2}'`

echo "start e2e test"
cd /root/script
/root/kubernetes/_output/bin/ginkgo $1 -noColor -nodes=20 --skip="named port" --focus="NetworkPolicy" /root/kubernetes/_output/bin/e2e.test -- --disable-log-dump --provider="skeleton" --kubeconfig="/root/.kube/config" > report/$filename

success=`cat /root/script/report/$filename | tail -n 5 | head -n 1 | awk '{print $3}'`
fail=`cat /root/script/report/$filename | tail -n 5 | head -n 1 | awk '{print $6}'`
usetime=`cat /root/script/report/$filename | tail -n 2 | head -n 1 | awk '{print $NF}'`

color="DF0000"
emoji=":x:"
if [ "$fail" == "0" ]; then
    color="2EA44F"
    emoji=":white_check_mark:"
fi

curl -X POST -H 'Content-type: application/json' https://hooks.slack.com/services/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  --data '{"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Finish Everoute K8S E2E Test, logs <http://192.168.28.7/'$filename' | `here`>"}}],"attachments":[{"color":"'$color'","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*Commit: * <https://github.com/everoute/everoute/commit/'$commitid'|`'$commitid'`> | main"}},{"type":"section","text":{"type":"mrkdwn","text":"*Result: * *'$success'* Passed  |  *'$fail'* Failed '$emoji'"}},{"type":"section","text":{"type":"mrkdwn","text":"*Usetime: * '$usetime'"}}]}]}'