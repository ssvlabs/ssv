#!/bin/bash
set -eo pipefail

# This script builds the SSV node for a specific target architecture
# Usage: build.sh <arch> <output_path>
# Example: build.sh amd64 /go/bin/ssvnode

# Get target architecture and output path
TARGET_ARCH=${1:-$(go env GOHOSTARCH)}
OUTPUT_PATH=${2:-/go/bin/ssvnode}

# Validate architecture
if [[ "$TARGET_ARCH" != "amd64" && "$TARGET_ARCH" != "arm64" ]]; then
  echo "Unsupported architecture: $TARGET_ARCH"
  echo "Supported architectures: amd64, arm64"
  exit 1
fi

echo "Building for $TARGET_ARCH..."

# Ensure the wrapper directory is in PATH
export PATH="/go/cross-compiler/$TARGET_ARCH/bin:$PATH"

# Set appropriate compiler environment variables
export CC=gcc
export CXX=g++

# Get version information
COMMIT=$(git rev-parse HEAD)
VERSION=$(git describe --tags $(git rev-list --tags --max-count=1) --always)

# Build with dynamic linking (more reliable across different environments)
if [[ "$TARGET_ARCH" == "amd64" ]]; then
  echo "Building amd64 binary..."
  CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -tags="blst_enabled" \
    -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION" \
    -o "$OUTPUT_PATH" \
    ./cmd/ssvnode
elif [[ "$TARGET_ARCH" == "arm64" ]]; then
  echo "Building arm64 binary..."
  CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build \
    -tags="blst_enabled" \
    -ldflags "-X main.Commit=$COMMIT -X main.Version=$VERSION" \
    -o "$OUTPUT_PATH" \
    ./cmd/ssvnode
fi

# Check if the binary was created successfully
if [[ -f "$OUTPUT_PATH" ]]; then
  echo "Build successful: $OUTPUT_PATH"
  file "$OUTPUT_PATH"
  ls -la "$OUTPUT_PATH"
else
  echo "Build failed: Binary not found at $OUTPUT_PATH"
  exit 1
fi
