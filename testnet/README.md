# ssv / local testnet

This folder contains scripts and tools running a local ssv network with eth1 and eth2 testnets.

## Prerequisites

Create `testnet` folder under `data` folder (`mkdir -p ./data/testnet`)

### Locally (MacOS/Linux)

Install [lighthouse](https://lighthouse-book.sigmaprime.io/setup.html) prerequisites

### VM (vagrant)

* [VirtualBox](https://www.virtualbox.org/wiki/Downloads)
* [Vagrant](https://www.vagrantup.com/downloads) + disksize plugin
```shell
$ brew install vagrant
$ vagrant plugin install vagrant-disksize
```

## Usage

Start:

```shell
$ make local-testnet-up
```

Run the following scripts to start a network:

```shell
cd ssv/testnet
export $(grep -v '^#' ./scripts/.env | xargs)
./scripts/lh_setup.sh
./scripts/lh_start_testnet.sh
./scripts/ssv_deploy_contracts.sh
./scripts/ssv_create_operators.sh
./scripts/ssv_setup_resources.sh
./scripts/ssv_create_validators.sh
./scripts/start_ssv.sh
```

Stop with

```shell
$ make local-testnet-down
```