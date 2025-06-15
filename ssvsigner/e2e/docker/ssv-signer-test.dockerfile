# Test Dockerfile for SSV-Signer E2E Testing
# Based on the main SSV-Signer Dockerfile
FROM golang:1.24 AS preparer

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
    curl \
    git \
    zip \
    unzip \
    g++ \
    gcc-aarch64-linux-gnu \
    bzip2 \
    make \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /go/src/github.com/ssvlabs/ssv/ssvsigner

# Cache dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go mod download && go mod verify

#
# Build executable binary
#
FROM preparer AS builder

# Copy files and build app
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    CGO_ENABLED=1 GOOS=linux go build \
    -tags="blst_enabled" \
    -ldflags "-linkmode external -extldflags \"-static -lm\"" \
    -o /go/bin/ssv-signer ./cmd/ssv-signer

#
# Final stage for testing
#
FROM golang:1.24 AS runner

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
    dnsutils ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /go/bin/ssv-signer /app/ssv-signer

# Create directories for test configuration
RUN mkdir -p /app/config /app/data

# Expose default port (can be overridden)
EXPOSE 8080

# Force using Go's DNS resolver
ENV GODEBUG="netdns=go"

# Default command - can be overridden in tests
ENTRYPOINT ["/app/ssv-signer"]