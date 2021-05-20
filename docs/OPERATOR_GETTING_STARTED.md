[<img src="./resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Operator Getting Started Guide

## Running a Local Network of Operators

#### 1. Clone Repository
```bash
$ git clone https://github.com/ethereum/eth2-ssv.git
```
#### 2. Build Binary
```bash
$ CGO_ENABLED=1 go build -o ./bin/ssvnode ./cmd/ssvnode/
```
#### 3. Split Validator Key 
Split key into 4 shares (can be more than 4 shares but at the moment it’s hard coded)
```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
$ ./bin/ssvnode export-keys --mnemonic={mnemonic} --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count {number of ssv nodes} --private-key {privateKey}
```
#### 4. Create Config Files

  ##### 4.1. Node Config

  Fill the required fields in [config.yaml](./config/example_share.yaml) file

  ##### 4.2. Shares Config

  Create 4 .yaml files with the corresponding configuration, based on the [template file](./config/example_share.yaml). \
  The files should be placed in the root directory of eth2-SSV project (`./share1.yaml`, `./share2.yaml`, etc.)

#### 5. Run a local network with 4 nodes
```bash
$ make docker-debug 
```
