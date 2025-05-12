#
# Prepare stage.
#
FROM golang:1.24-alpine AS prepare
WORKDIR /app

# Copy the go.mod and go.sum first and download the dependencies.
# This layer will be cached unless these files change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go mod download

#
# Build stage.
#
FROM prepare AS build
WORKDIR /app

# Install build dependencies required for CGO
RUN apk add --no-cache musl-dev gcc g++ libstdc++

# Copy the rest of the source code
COPY . .

# Build the binary with caching
ENV CGO_ENABLED=1
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,mode=0755,target=/go/pkg \
    go build -o /bin/ssv-e2e ./cmd/ssv-e2e

#
# Run stage.
#
FROM alpine:3.18
WORKDIR /app

# Copy the built binary from the previous stage
COPY --from=build /bin/ssv-e2e /bin/ssv-e2e

RUN apk add --no-cache curl

ENTRYPOINT ["/bin/ssv-e2e"]
