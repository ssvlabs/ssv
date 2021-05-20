[<img src="./resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Development Guide

## Usage

### Common Commands

#### Build
```bash
$ CGO_ENABLED=1 go build -o ./bin/ssvnode ./cmd/ssvnode/
```

#### Test
```bash
$ make full-test
```

#### Lint
```bash
$ make lint-prepare
$ make lint
```

#### Splitting a key

We split an eth2 BLS validator key into shares via Shamir-Secret-Sharing(SSS) to be used between the SSV nodes.

```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
$ ./bin/ssvnode export-keys --mnemonic={mnemonic} --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count {# of ssv nodes} --private-key {privateKey}
```

### Example .env file
```
   NETWORK=pyrmont
   DISCOVERY_TYPE=<mdns for local network, empty for discov5 remote>
   STORAGE_PATH=<example ./data/db/node_1/2/3/4>
   BOOT_NODE_EXTERNAL_IP=
   BOOT_NODE_PRIVATE_KEY=
   BEACON_NODE_ADDR= <can use eth2-4000-prysm-ext.stage.bloxinfra.com:80>
   NODE_ID=
   VALIDATOR_PUBLIC_KEY=
   SSV_PRIVATE_KEY=
   PUBKEY_NODE_1=
   PUBKEY_NODE_2=
   PUBKEY_NODE_3=
   PUBKEY_NODE_4=
```
For a 4 node SSV network, 4 .env.node.<1/2/3/4> files need to be created.

## Standards

Please make sure your contributions adhere to our coding guidelines:

* Code must adhere to the official Go [formatting](https://golang.org/doc/effective_go.html#formatting)
  guidelines (i.e. uses [gofmt](https://golang.org/cmd/gofmt/)).
* Code must be documented adhering to the official Go [commentary](https://golang.org/doc/effective_go.html#commentary)
  guidelines.
* Pull requests need to be based on and opened against the `stage` branch.
