#
# STEP 1: Prepare environment
#
FROM golang:1.26@sha256:670bb5aa5349ae03b8516764718486f4f16399134bdc9acd0c46a23f043ab05c AS preparer

RUN apt-get update && apt upgrade -y && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  make curl git zip unzip wget dnsutils g++ gcc-aarch64-linux-gnu                 \
  && rm -rf /var/lib/apt/lists/*

RUN go version
ENV GO111MODULE=on
RUN go get github.com/go-delve/delve/cmd/dlv
RUN go get -u github.com/cosmtrek/air@v1.27.8

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod .
COPY go.sum .
COPY ssvsigner/go.mod ssvsigner/go.sum ./ssvsigner/
RUN go mod download

COPY . .

CMD air
