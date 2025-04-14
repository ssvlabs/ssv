# Global build arguments
#
## Note BUILDPLATFORM, TARGETPLATFORM and TARGETARCH are crucial variables:
##   see https://docs.docker.com/reference/dockerfile/#automatic-platform-args-in-the-global-scope
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETARCH

#
# STEP 1: Base image with common dependencies
#
FROM --platform=${BUILDPLATFORM} golang:1.24 AS base

# Install common essential build tools
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
      curl git ca-certificates make \
      zip unzip bzip2 g++ && \
    rm -rf /var/lib/apt/lists/*

# Install cross-compilation tools only when needed
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends build-essential && \
    if [ "${TARGETARCH}" = "arm64" ]; then \
        apt-get install -yq --no-install-recommends \
        gcc-aarch64-linux-gnu libc6-dev-arm64-cross binutils-aarch64-linux-gnu crossbuild-essential-arm64; \
    elif [ "${TARGETARCH}" = "amd64" ]; then \
        apt-get install -yq --no-install-recommends \
        gcc-x86-64-linux-gnu libc6-dev-amd64-cross binutils-x86-64-linux-gnu crossbuild-essential-amd64; \
    else \
        echo "Building for architecture: ${TARGETARCH}" >&2; \
    fi && \
    rm -rf /var/lib/apt/lists/*

# Set working directory for the build
WORKDIR /go/src/github.com/ssvlabs/ssv/

# Cache dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go mod download && go mod verify

#
# STEP 2: Build executable binary with cross-compilation
#
FROM base AS builder

# Copy source code
COPY . .

# Build the binary for the target architecture (arm64/amd64)
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    COMMIT=$(git rev-parse HEAD) && \
    VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always) && \
    echo "Building version: $VERSION (commit: $COMMIT) for $TARGETARCH" && \
    CC="" && \
    if [ "$TARGETARCH" = "amd64" ]; then \
      CC=x86_64-linux-gnu-gcc; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
      CC=aarch64-linux-gnu-gcc; \
    fi && \
    CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH CC=$CC \
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

# Force using Go's DNS resolver because Alpine's DNS resolver (when netdns=cgo) may cause issues
ENV GODEBUG="netdns=go"

#ENTRYPOINT ["/go/bin/ssvnode"]
