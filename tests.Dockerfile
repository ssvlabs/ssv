#
# STEP 1: Prepare environment
#
FROM golang:1.26@sha256:d7722066f0b60ceccb6c0643cbed1f5f9e15506ac237146c95333504d7805d89 AS preparer

RUN apt-get update && apt upgrade -y && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  make curl git zip unzip wget dnsutils g++ gcc-aarch64-linux-gnu                 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod .
COPY go.sum .
COPY ssvsigner/go.mod ssvsigner/go.sum ./ssvsigner/
RUN go mod download

COPY . .
