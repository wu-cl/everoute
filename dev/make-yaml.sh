
find deploy -name "*.yaml" | grep -v ^deploy/everoute.yaml$ | sort -u | xargs cat | cat > dev/everoute.yaml

sed -i 's/image: everoute\/release/image: harbor.smartx.com\/everoute\/dev:latest/g' dev/everoute.yaml

sed -i 's/imagePullPolicy: IfNotPresent/#imagePullPolicy: IfNotPresent/g' dev/everoute.yaml
