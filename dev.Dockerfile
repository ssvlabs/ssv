#
# STEP 1: Prepare environment
#
FROM golang:1.26@sha256:37cfcca63222ec499bec5e8ad7aad52dbf7529db36d5812b8eae2f0621aa92d6 AS preparer

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
