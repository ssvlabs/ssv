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

# install jemalloc
WORKDIR /tmp/jemalloc-temp
RUN curl -s -L "https://github.com/jemalloc/jemalloc/releases/download/5.3.0/jemalloc-5.3.0.tar.bz2" -o jemalloc.tar.bz2 \
  && tar xjf ./jemalloc.tar.bz2
RUN cd jemalloc-5.3.0 \
  && ./configure --with-jemalloc-prefix='je_' --with-malloc-conf='background_thread:true,metadata_thp:auto' \
  && make && make install

RUN go version

WORKDIR /go/src/github.com/ssvlabs/ssv/
COPY go.mod .
COPY go.sum .
RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  go mod download

ARG APP_VERSION
#
# STEP 2: Build executable binary
#
FROM preparer AS builder

# Copy files and install app
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,mode=0755,target=/go/pkg \
  COMMIT=$(git rev-parse HEAD) && \
  VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always) && \
  CGO_ENABLED=1 GOOS=linux go install \
  -tags="blst_enabled,jemalloc,allocator" \
  -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION -linkmode external -extldflags \"-static -lm\"" \
  ./cmd/ssvnode

#
# STEP 3: Prepare image to run the binary
#
# IMPORTANT: before upgrading to go 1.23 or higher make sure the following issue has been resolved:
# https://github.com/golang/go/issues/69978
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

