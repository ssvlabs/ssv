#
# STEP 1: Prepare environment with cross-compilation tools
#
FROM --platform=$BUILDPLATFORM golang:1.22 AS preparer

# Copy our build scripts
COPY tools/cross-compiler/scripts /tmp/scripts
RUN chmod +x /tmp/scripts/*.sh

# Install dependencies for cross-compilation
RUN /tmp/scripts/install_deps.sh

# Create compiler wrappers to handle cross-compilation flags
RUN /tmp/scripts/create_wrappers.sh

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  go mod download


#
# STEP 2: Build executable binary
#
FROM preparer AS builder

## Note BUILDPLATFORM and TARGETARCH are crucial variables:
##   see https://docs.docker.com/reference/dockerfile/#automatic-platform-args-in-the-global-scope
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETARCH

# Copy files and app sources
COPY . .

# Build the binary using our build script
RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  /tmp/scripts/build.sh "${TARGETARCH}" /go/bin/ssvnode

# Show the built binary for verification
RUN ls -la /go/bin/

#
# STEP 3: Prepare minimal image to run the binary
#
FROM golang:1.22 AS runner

RUN apt-get update     && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  dnsutils && \
  rm -rf /var/lib/apt/lists/*

WORKDIR /

COPY --from=builder /go/bin/ssvnode /go/bin/ssvnode
COPY ./Makefile .env* ./
COPY config/* ./config/

# Expose ports
EXPOSE 5678 5000 4000/udp

# Force using Go's DNS resolver because Alpine's DNS resolver (when netdns=cgo) may cause issues.
ENV GODEBUG="netdns=go"

#ENTRYPOINT ["/go/bin/ssvnode"]
