#!/usr/bin/env bash

export DEBIAN_FRONTEND=noninteractive
sudo apt-get update -y

## install deps
DEBIAN_FRONTEND=noninteractive; sudo apt-get install -y \
  git gcc g++ make cmake pkg-config curl gnupg ca-certificates

## nodejs
curl -sL https://deb.nodesource.com/setup_16.x | sudo -E bash -
sudo apt-get install -y nodejs
## yarn
curl -sL https://dl.yarnpkg.com/debian/pubkey.gpg | gpg --dearmor | sudo tee /usr/share/keyrings/yarnkey.gpg
echo "deb [signed-by=/usr/share/keyrings/yarnkey.gpg] https://dl.yarnpkg.com/debian stable main" | sudo tee /etc/apt/sources.list.d/yarn.list
sudo apt-get update -y && sudo apt-get install yarn -y
#sudo apt install openjdk-11-jre

## rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs --output rustup.sh \
    && chmod u+x ./rustup.sh \
    && ./rustup.sh -y
source ,/.cargo/env

# docker
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
echo \
  "deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get -y update && sudo apt-get -y install docker-ce docker-ce-cli containerd.io
