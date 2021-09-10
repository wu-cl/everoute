FROM golang:1.15 as builder

# Download deps
RUN mkdir -p $GOPATH/src/lynx/pkg
WORKDIR $GOPATH/src/lynx

ADD go.mod go.sum ./
RUN go mod download

RUN mkdir -p /opt/lynx/bin
ADD ./build/script/* /opt/lynx/bin/
ADD . $GOPATH/src/lynx

# Build
RUN --mount=type=cache,target=/root/.cache/go-build go build -o bin/lynx-controller cmd/lynx-controller/main.go

RUN --mount=type=cache,target=/root/.cache/go-build go build -o bin/lynx-agent cmd/lynx-agent/*.go

RUN --mount=type=cache,target=/root/.cache/go-build go build -o bin/lynx-cni cmd/lynx-cni/*.go

RUN cp $GOPATH/src/lynx/bin/* /opt/lynx/bin/

#FROM alpine
FROM ubuntu:20.04

#RUN apk update && apk add openvswitch
RUN apt update && apt install -y openvswitch-switch iproute2 iptables
RUN apt clean

RUN mkdir -p /opt/lynx/bin
COPY --from=builder /opt/lynx/bin/* /opt/lynx/bin/

WORKDIR /opt/lynx/bin
ENV PATH=${PATH}:/opt/lynx/bin
