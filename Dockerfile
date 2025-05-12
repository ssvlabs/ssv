#
# STEP 1: Base image with common dependencies
#
FROM golang:1.24 AS base

# Install essential build tools
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
      curl git ca-certificates make \
      zip unzip bzip2 g++ && \
    rm -rf /var/lib/apt/lists/*

# Set working directory for the build
WORKDIR /go/src/github.com/ssvlabs/ssv/

# Cache dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go mod download && go mod verify

#
# STEP 2: Build executable binary
#
FROM base AS builder

## Note TARGETARCH is crucial variable:
##   see https://docs.docker.com/reference/dockerfile/#automatic-platform-args-in-the-global-scope
ARG TARGETARCH

# Copy source code
COPY . .

# Build the application
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    COMMIT=$(git rev-parse HEAD) && \
    VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always) && \
    CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    go install \
      -tags="blst_enabled" \
      -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION -linkmode external -extldflags '-static -lm'" \
      ./cmd/ssvnode

#
# STEP 3: Prepare image to run the binary
#
FROM golang:1.24 AS runner

# Install minimal runtime dependencies
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
    dnsutils && \
    rm -rf /var/lib/apt/lists/*

# Copy binary and configuration
WORKDIR /
COPY --from=builder /go/bin/ssvnode /go/bin/ssvnode
COPY ./Makefile .env* ./
COPY config/* ./config/

# Expose port for load balancing
EXPOSE 5678 5000 4000/udp

# Force using Go's DNS resolver because Alpine's DNS resolver (when netdns=cgo) may cause issues.
ENV GODEBUG="netdns=go"

#ENTRYPOINT ["/go/bin/ssvnode"]
