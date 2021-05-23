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

#### Splitting a Validator Key

We split an eth2 BLS validator key into shares via Shamir-Secret-Sharing(SSS) to be used between the SSV nodes.

```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
$ ./bin/ssvnode export-keys --mnemonic={mnemonic} --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count {# of ssv nodes} --private-key {privateKey}
```

#### Generating an Operator Key

```bash
$ ./bin/ssvnode generate-operator-keys
```

### Config Files

Config files are located in `./config` directory:

#### Node Config 

Specifies general configuration regards the current node. \
Example yaml - [config.yaml](../config/config.yaml)

#### Shares Config

For a 4 node SSV network, 4 share<nodeId>.yaml files need to be created, based on the [template file](../config/example_share.yaml). \
E.g. `./config/share1.yaml`, `./config/share2.yaml`, etc.

## Standards

Please make sure your contributions adhere to our coding guidelines:

* Code must adhere to the official Go [formatting](https://golang.org/doc/effective_go.html#formatting)
  guidelines (i.e. uses [gofmt](https://golang.org/cmd/gofmt/)).
* Code must be documented adhering to the official Go [commentary](https://golang.org/doc/effective_go.html#commentary)
  guidelines.
* Pull requests need to be based on and opened against the `stage` branch.
