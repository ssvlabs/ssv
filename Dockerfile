#
# STEP 1: Prepare environment
#
FROM golang:1.22 AS preparer

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

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  go mod download


#
# STEP 2: Build executable binary
#
FROM preparer AS builder

## Note TARGETARCH is a crucial variable:
##   see https://docs.docker.com/reference/dockerfile/#automatic-platform-args-in-the-global-scope
ARG TARGETARCH

# Copy files and install app
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  COMMIT=$(git rev-parse HEAD) && \
  VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always) && \
  CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go install \
  -tags="blst_enabled" \
  -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION -linkmode external -extldflags \"-static -lm\"" \
  ./cmd/ssvnode

#
# STEP 3: Prepare image to run the binary
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


# Expose port for load balancing
EXPOSE 5678 5000 4000/udp

# Force using Go's DNS resolver because Alpine's DNS resolver (when netdns=cgo) may cause issues.
ENV GODEBUG="netdns=go"

#ENTRYPOINT ["/go/bin/ssvnode"]

