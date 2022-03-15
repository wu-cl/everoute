#!/bin/bash

function wait_for_up() {
  for i in {1..100}; do
    status=$(kubectl get po -Aowide | grep ${1} | grep -v Running |  wc -l)
    if [[ x${status} == x"0" ]]; then
      echo "success wait ${1} setup"
      break
    fi
    if [[ ${i} == 100 ]]; then
      echo "failed wait for ${1} setup"
      exit 1
    fi
    sleep 1
    echo "${i} times, wait for ${1} setup ..."
  done
}

function wait_for_down() {
  for i in {1..100}; do
    status=$(kubectl get po -Aowide | grep ${1} |  wc -l)
    if [[ x${status} == x"0" ]]; then
      echo "success wait ${1} uninstall"
      break
    fi
    if [[ ${i} == 100 ]]; then
      echo "failed wait for ${1} uninstall"
      exit 1
    fi
    sleep 1
    echo "${i} times, wait for ${1} uninstall ..."
  done
}

cd /root/everoute

kubectl delete -f /root/everoute/dev/everoute.yaml

git checkout .
git pull origin main

cat build/images/release/Dockerfile | \
sed 's/RUN make bin/RUN --mount=type=cache,target=\/root\/.cache\/go-build go build -o bin\/everoute-controller cmd\/everoute-controller\/main.go\nRUN --mount=type=cache,target=\/root\/.cache\/go-build go build -o bin\/everoute-agent cmd\/everoute-agent\/*.go\nRUN --mount=type=cache,target=\/root\/.cache\/go-build go build -o bin\/everoute-cni cmd\/everoute-cni\/*.go/g' \
| cat > dev/Dockerfile

docker build -f dev/Dockerfile -t harbor.smartx.com/everoute/dev:latest .
docker push harbor.smartx.com/everoute/dev:latest

make yaml

cp deploy/everoute.yaml dev/everoute.yaml

sed -i 's/image: everoute\/release/image: harbor.smartx.com\/everoute\/dev:latest/g' dev/everoute.yaml
sed -i 's/imagePullPolicy: IfNotPresent/#imagePullPolicy: IfNotPresent/g' dev/everoute.yaml


wait_for_down "everoute"
kubectl apply -f dev/everoute.yaml
sleep 3
wait_for_up "everoute"