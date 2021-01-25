#
# STEP 1: Prepare environment
#
FROM golang:1.15 AS preparer

RUN apt-get update                                                        && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
    curl git zip unzip wget g++ python gcc-aarch64-linux-gnu                 \
  && rm -rf /var/lib/apt/lists/*

RUN go version
RUN python --version

WORKDIR /go/src/github.com/bloxapp/ssv/
COPY go.mod .
COPY go.sum .
RUN go mod download

#
# STEP 2: Build executable binary
#
FROM preparer AS builder

# Copy files and install app
COPY . .

RUN go get -d -v ./...
RUN CGO_ENABLED=1 GOOS=linux go install -a -tags blst_enabled -ldflags "-linkmode external -extldflags \"-static -lm\"" ./cmd/ssvcli

#
# STEP 3: Prepare image to run the binary
#
FROM alpine:3.12 AS runner

# Install ca-certificates, bash
RUN apk -v --update add ca-certificates bash && \
    rm /var/cache/apk/*

COPY --from=builder /go/bin/ssvcli /go/bin/ssvcli

# Expose port for load balancing
EXPOSE 5678

ENTRYPOINT ["/go/bin/ssvcli"]
