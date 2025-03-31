#
# Global build arguments
#
## Note BUILDPLATFORM and TARGETARCH are crucial variables:
##   see https://docs.docker.com/reference/dockerfile/#automatic-platform-args-in-the-global-scope
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETARCH

#
# STEP 1: Prepare environment
#
FROM golang:1.22 AS preparer

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      curl git zip unzip g++ gcc-aarch64-linux-gnu bzip2 make && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg,mode=0755 \
    go mod download

#
# STEP 2: Build executable binary
#
FROM preparer AS builder

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg,mode=0755 \
    COMMIT=$(git rev-parse HEAD) && \
    VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always) && \
    CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go install -tags="blst_enabled" \
      -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION -linkmode external -extldflags \"-static -lm\"" \
      ./cmd/ssvnode

#
# STEP 3: Prepare runtime dependencies
#
FROM debian:stable-slim AS deps

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      dnsutils ca-certificates && \
    rm -rf /var/lib/apt/lists/*

#
# STEP 4: Final minimal image
#
FROM scratch AS runner

# Copy SSL certificates from deps stage
COPY --from=deps /etc/ssl/certs /etc/ssl/certs

WORKDIR /

# Copy the compiled binary and configuration files
COPY --from=builder /go/bin/ssvnode /go/bin/ssvnode
COPY ./Makefile .env* ./
COPY config/ ./config/

# Expose necessary ports
EXPOSE 5678 5000 4000/udp

# Force using Go's DNS resolver
ENV GODEBUG="netdns=go"

# Start the SSV node when container runs
#ENTRYPOINT ["/go/bin/ssvnode"]
