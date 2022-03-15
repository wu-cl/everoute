FROM golang:1.16 as builder

RUN apt update && apt install -y openvswitch-switch iptables iproute2 tcpdump && rm -rf /var/lib/apt/lists/*

# Download deps
RUN mkdir -p $GOPATH/src/everoute
WORKDIR $GOPATH/src/everoute

RUN mkdir -p /opt/everoute/bin

# download CNI plugins
ARG CNI_BINARIES_VERSION=v1.0.0
ARG TARGETARCH
RUN wget -q -O - https://github.com/containernetworking/plugins/releases/download/$CNI_BINARIES_VERSION/cni-plugins-linux-$TARGETARCH-$CNI_BINARIES_VERSION.tgz | tar xz -C /opt/everoute/bin ./host-local ./loopback ./portmap

ADD go.mod go.sum ./
RUN go mod download

ADD ./build/script/* /opt/everoute/bin/
ADD . $GOPATH/src/everoute

