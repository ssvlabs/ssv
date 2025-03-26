#!/bin/bash
set -eo pipefail

echo "Installing cross-compilation dependencies..."

# Update repositories and install basic tools
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  curl \
  git \
  zip \
  unzip \
  g++ \
  make \
  cmake \
  bzip2 \
  autoconf \
  libtool \
  pkg-config \
  ca-certificates \
  xz-utils

# Install cross-compilers and libs
DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
  gcc-aarch64-linux-gnu \
  g++-aarch64-linux-gnu \
  gcc-x86-64-linux-gnu \
  g++-x86-64-linux-gnu \
  libc6-dev-amd64-cross \
  libc6-dev-arm64-cross \
  libgcc-s1-amd64-cross \
  libstdc++6-amd64-cross \
  libgcc-s1-arm64-cross \
  libstdc++6-arm64-cross

# Clean up apt cache
rm -rf /var/lib/apt/lists/*

echo "Dependencies installation completed."
