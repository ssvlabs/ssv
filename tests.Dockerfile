#
# STEP 1: Prepare environment
#
FROM golang:1.24@sha256:5e9d14d681c3224276f0c8e318525ef6fc96b47fbcbb89f8bec0e402e18ea8bf AS preparer

RUN apt-get update && apt upgrade -y && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  make curl git zip unzip wget dnsutils g++ gcc-aarch64-linux-gnu                 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
