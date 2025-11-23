#
# STEP 1: Prepare environment
#
FROM golang:1.24@sha256:7b13449f08287fdb53114d65bdf20eb3965e4e54997903b5cb9477df0ea37c12 AS preparer

RUN apt-get update && apt upgrade -y && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  make curl git zip unzip wget dnsutils g++ gcc-aarch64-linux-gnu                 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
