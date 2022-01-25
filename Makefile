CONTROLLER_GEN=$(shell which controller-gen)
APISERVER_BOOT=$(shell which apiserver-boot)

all: codegen manifests bin

bin: controller agent cni

images: image image-generate

image:
	docker buildx build -f build/images/release/Dockerfile -t everoute/release .

image-generate:
	docker buildx build -f build/images/generate/Dockerfile -t everoute/generate ./build/images/generate/

yaml:
	find deploy -name "*.yaml" | grep -v ^deploy/everoute.yaml$ | sort -u | xargs cat | cat > deploy/everoute.yaml

generate: codegen gqlgen protopb manifests yaml apidocs-gen

docker-generate:
	$(eval WORKDIR := /go/src/github.com/everoute/everoute)
	docker run --rm -iu 0:0 -w $(WORKDIR) -v $(CURDIR):$(WORKDIR) everoute/generate make generate

controller:
	CGO_ENABLED=0 go build -o bin/everoute-controller cmd/everoute-controller/main.go

agent:
	CGO_ENABLED=0 go build -o bin/everoute-agent cmd/everoute-agent/*.go

exporter:
	CGO_ENABLED=0 go build -o bin/everoute-exporter cmd/everoute-exporter/*.go

visualization:
	CGO_ENABLED=0 go build -o bin/everoute-visualization cmd/everoute-visualization/*.go

cni:
	CGO_ENABLED=0 go build -o bin/everoute-cni cmd/everoute-cni/*.go

e2e-tools:
	CGO_ENABLED=0 go build -o bin/e2ectl tests/e2e/tools/e2ectl/*.go
	CGO_ENABLED=0 go build -o bin/net-utils tests/e2e/tools/net-utils/*.go

test:
	go test ./plugin/tower/pkg/controller/... ./pkg/... -v

cover-test:
	go test ./plugin/tower/pkg/controller/... ./pkg/... -coverprofile=coverage.out \
		-coverpkg=./pkg/...,./plugin/tower/pkg/controller/...

race-test:
	go test ./plugin/tower/pkg/controller/... ./pkg/... -race

e2e-test:
	go test ./tests/e2e/...

# Generate deepcopy, client, openapi codes
codegen: manifests
	$(APISERVER_BOOT) build generated --generator openapi --generator client --generator deepcopy --copyright hack/boilerplate.go.txt \
		--api-versions agent/v1alpha1 \
		--api-versions group/v1alpha1 \
		--api-versions security/v1alpha1

# Generate plugin-tower gql codes
gqlgen:
	cd plugin/tower/pkg/server/fake/ && gqlgen generate

protopb:
	protoc -I=. --go_out=plugins=grpc:.  pkg/apis/cni/v1alpha1/cni.proto
	protoc -I=. --go_out=plugins=grpc:.  pkg/apis/exporter/v1alpha1/exporter.proto

apidocs-gen:
	$(eval PATH := $$(PATH):$(shell go env GOPATH)/bin)
	which gen-crd-api-reference-docs || go install github.com/ahmetb/gen-crd-api-reference-docs@v0.3.0
	gen-crd-api-reference-docs --config docs/assets/apidocs-gen.json \
		--out-file docs/content/en/docs/reference/apidocs.html --template-dir docs/assets/templates/ \
		--api-dir ./pkg/apis/security/v1alpha1

deploy-test:
	bash hack/deploy.sh

deploy-test-clean:
	bash hack/undeploy.sh

# Generate CRD manifests
manifests:
	$(CONTROLLER_GEN) crd paths="./pkg/apis/..." output:crd:dir=deploy/crds output:stdout

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

clean:
	$(APISERVER_BOOT) build generated clean
