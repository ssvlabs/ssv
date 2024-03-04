#
# STEP 1: Prepare environment
#
FROM golang:1.20.7 AS preparer

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
RUN curl -s -L "https://github.com/jemalloc/jemalloc/releases/download/5.2.1/jemalloc-5.2.1.tar.bz2" -o jemalloc.tar.bz2 \
  && tar xjf ./jemalloc.tar.bz2
RUN cd jemalloc-5.2.1 \
  && ./configure --with-jemalloc-prefix='je_' --with-malloc-conf='background_thread:true,metadata_thp:auto' \
  && make && make install

RUN go version

WORKDIR /go/src/github.com/bloxapp/ssv/
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
FROM alpine:3.18.3 AS runner

# Install ca-certificates, bash
RUN apk -v --update add \
  ca-certificates \
  bash \
  make \
  bind-tools && \
  rm /var/cache/apk/*

COPY --from=builder /go/bin/ssvnode /go/bin/ssvnode
COPY ./Makefile .env* ./
COPY config/* ./config/


# Expose port for load balancing
EXPOSE 5678 5000 4000/udp

# Force using Go's DNS resolver because Alpine's DNS resolver (when netdns=cgo) may cause issues.
ENV GODEBUG="netdns=go"

#ENTRYPOINT ["/go/bin/ssvnode"]
