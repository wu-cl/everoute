FROM golang:1.15 as builder

# Download deps
RUN mkdir -p $GOPATH/src/lynx
WORKDIR $GOPATH/src/lynx

COPY go.mod go.sum ./
RUN go mod download

COPY . $GOPATH/src/lynx
RUN mkdir -p /opt/lynx/bin
RUN cp $GOPATH/src/lynx/build/script/* /opt/lynx/bin/

# Build
RUN make controller
RUN make agent
RUN make cni
RUN cp $GOPATH/src/lynx/bin/* /opt/lynx/bin/

#FROM alpine
FROM ubuntu:20.04

#RUN apk update && apk add openvswitch
RUN apt update && apt install -y openvswitch-switch && apt install -y iproute2

COPY --from=builder /opt/lynx/bin/* /opt/lynx/bin/

WORKDIR /opt/lynx/bin
ENV PATH=${PATH}:/opt/lynx/bin
