#
# STEP 1: Prepare environment
#
FROM golang:1.27@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS preparer

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
