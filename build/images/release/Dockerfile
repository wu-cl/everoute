FROM golang:1.15 as builder

# Download deps
RUN mkdir -p $GOPATH/src/lynx
WORKDIR $GOPATH/src/lynx

COPY go.mod go.sum ./
RUN go mod download

RUN mkdir -p /opt/lynx/bin
COPY ./build/script/* /opt/lynx/bin/
COPY . $GOPATH/src/lynx

# Build
RUN make controller
RUN make agent
RUN make cni
RUN cp $GOPATH/src/lynx/bin/* /opt/lynx/bin/

#FROM alpine
FROM ubuntu:20.04

#RUN apk update && apk add openvswitch
RUN apt update && apt install -y openvswitch-switch iproute2 iptables
RUN apt clean

COPY --from=builder /opt/lynx/bin/* /opt/lynx/bin/

WORKDIR /opt/lynx/bin
ENV PATH=${PATH}:/opt/lynx/bin
