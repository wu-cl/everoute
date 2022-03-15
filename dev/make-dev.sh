
cat build/images/release/Dockerfile | \
sed 's/RUN make bin/RUN --mount=type=cache,target=\/root\/.cache\/go-build go build -o bin\/everoute-controller cmd\/everoute-controller\/main.go\nRUN --mount=type=cache,target=\/root\/.cache\/go-build go build -o bin\/everoute-agent cmd\/everoute-agent\/*.go\nRUN --mount=type=cache,target=\/root\/.cache\/go-build go build -o bin\/everoute-cni cmd\/everoute-cni\/*.go/g' \
| cat > dev/Dockerfile

docker build -f dev/Dockerfile -t harbor.smartx.com/everoute/dev:latest .
docker push harbor.smartx.com/everoute/dev:latest

make yaml

cp deploy/everoute.yaml dev/everoute.yaml

sed -i 's/image: everoute\/release/image: harbor.smartx.com\/everoute\/dev:latest/g' dev/everoute.yaml

sed -i 's/imagePullPolicy: IfNotPresent/#imagePullPolicy: IfNotPresent/g' dev/everoute.yaml