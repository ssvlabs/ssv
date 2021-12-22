#!/usr/bin/env bash

sudo npm i -g ganache-cli@latest typescript

mkdir -p "$SSV_TESTNET_DIR" > /dev/null

## lighthouse
cd "$SSV_TESTNET_DIR" && git clone https://github.com/sigp/lighthouse.git \
  && cd lighthouse && make && make install-lcli \
