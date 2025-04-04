#
# STEP 1: Prepare environment
#
FROM golang:1.24 AS preparer

RUN apt-get update                                                        && \
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

RUN go version

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
FROM preparer AS builder

# Copy files and install app
COPY .. .

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  COMMIT=$(git rev-parse HEAD) && \
  VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always) && \
  CGO_ENABLED=1 GOOS=linux go install -C ./ssvsigner/cmd/ssv-signer \
  -tags="blst_enabled" \
  -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION -linkmode external -extldflags \"-static -lm\"" 

##
# STEP 3: Prepare image to run the binary
#
FROM golang:1.24 AS runner

RUN apt-get update     && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  dnsutils && \
  rm -rf /var/lib/apt/lists/*

WORKDIR /

COPY --from=builder /go/bin/ssv-signer /go/bin/ssv-signer

# Expose port for load balancing
EXPOSE 8080

# Force using Go's DNS resolver because Alpine's DNS resolver (when netdns=cgo) may cause issues.
ENV GODEBUG="netdns=go"

ENTRYPOINT ["/go/bin/ssv-signer"]
