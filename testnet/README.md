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

Start a network (within the VM):

```shell
cd ssv/testnet/scripts && ./setup.sh
```

Stop (within the VM):

```shell
cd $SSV_TESTNET_DIR && docker-compose down
cd $SSV_DIR && ./testnet/scripts/lh_stop_testnet.sh
```

Destroy the entire VM with:

```shell
$ make local-testnet-down
```