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

#### 3. Generate Operator Key

See [Dev Guide > Generating an Operator Key](./DEV_GUIDE.md#generating-an-operator-key).

#### 4. Split Validator Key

See [Dev Guide > Splitting a Validator Key](./DEV_GUIDE.md#splitting-a-validator-key).

#### 5. Create Config Files

  ##### 5.1. Node Config

  Fill the required fields in [config.yaml](../config/config.yaml) file

  ##### 5.2. Shares Config

  Create 4 .yaml files with the corresponding configuration, based on the [template file](../config/example_share.yaml). \
  The files should be placed in the `./config` directory (`./config/share1.yaml`, `./config/share2.yaml`, etc.)

#### 6. Run a local network with 4 nodes
```bash
$ make docker-debug 
```

## Setting AWS Server for Operator

### 1. Setup

Create a server of your choice and expose on ports 12000 UDP and 13000 TCP.
- In the search bar search for "ec2"
- Launch new instance
- Choose "ubuntu server 20.04"
- Choose "t2.micro" (free tire)
- Skip to "security group" section
- make sure you have 3 rules. UDP, TCP and SSH -
  ![security_permission](./resources/security_permission.png)
- after launch, add new key pair and download the ssh file
- launch instance

### 2. Login with SSH

```
$ cd ./{path to where the ssh downloaded}

$ chmod 400 {ssh file name}

$ ssh -i {ssh file name} ubuntu@{server public ip}
```

### 3. Installation Script

Download and run the installation script.

```
$ sudo su

$ wget https://raw.githubusercontent.com/ethereum/eth2-ssv/stage/install.sh

$ chmod +x install.sh

$ ./install.sh
```

### 4. Create a Configuration File

Fill all the placeholders (e.g. `<ETH 2.0 node>` or `<privkey of the operator>`) with actual values,
and run the commands below to create a `config.yaml` file.

General configuration
```
$ yq e -n '.db = {"Path": "<db folder>", "Type": "badger-db"}' \
  | yq e '.Network = "pyrmont"' - \
  | yq e '.BeaconNodeAddr = "<ETH 2.0 node>"' - \
  | yq e '.ETH1Addr = "<ETH1 node>"' - \
  | yq e '.HostAddress = "<host public ip>"' - \
  | yq e '.OperatorKey = "<privkey of the operator>"' - \
  | tee config.yaml
```

SSV configuration
```
$ yq e -n '.ssv.ValidatorOptions.Shares[0].NodeID = "<node id>"' \
  | yq e '.ssv.ValidatorOptions.Shares[0].PublicKey = "<validator public key>"' - \
  | yq e '.ssv.ValidatorOptions.Shares[0].ShareKey = "<share private key>"' - \
  | yq e '.ssv.ValidatorOptions.Shares[0].Committee.<share_public_key1> = "<node1 id>"' - \
  | yq e '.ssv.ValidatorOptions.Shares[0].Committee.<share_public_key2> = "<node2 id>"' - \
  | yq e '.ssv.ValidatorOptions.Shares[0].Committee.<share_public_key3> = "<node4 id>"' - \
  | yq e '.ssv.ValidatorOptions.Shares[0].Committee.<share_public_key4> = "<node4 id>"' - \
  | tee -a config.yaml
```



`config.yaml` example:

```yaml
db:
  Path: ./data/db/node_1
  Type: badger-db

ssv:
  OperatorPrivKey: 0c2232bbb6e67faee1f99ad380db9c7a8d126712047d1afdfff696bb9cf04456
  ValidatorOptions:
    Shares:
      - NodeID: 1
        PublicKey: 7ef3a53383a2c9b9ab0ab5437985ac443a8d50bf50b5f69eeaf9850285aeaad703beff14e3d15b4e6b5702f446a97db4
        ShareKey: 0a77c5f6b6e67faee1f99ad380db9c7a8d126712047d1afdfff696bb9cf08db4
        Committee:
          55b363beffe6de380adf56e04c4186fa36a6ecaf00548ff3538f7de8b1a9b58a826771436640af6038a57a2d79c8ae11: 1
          69d2fa1b2c1e0e2c957bb5bdcc9fd4666640c533247aa363327f185f406344d6e5cb142bcaff6b87ff571113cc71669f: 2
          327090f801cc7b19058f3e95d9279856c7ec1b49ef123b8cbefc48ad88f925eba8eeec3c19668d21cb036b1a37ef05be: 3
          a7cdcf1acc29fc313e11a0d47f83c25759fe91c8cd3c6396ac28767a2efce4e5af7777a66a55634a03fe0372549b725c: 4

Network: pyrmont
BeaconNodeAddr: prysm.stage.blox.com:80
ETH1Addr: ws://eth1.stage.blox.com/ws
HostAddress: 84.220.13.151
```

### 5. Start Node in Docker

```
$ docker run -d --restart unless-stopped --name=ssv_node -e CONFIG_PATH=./config.yaml -p 13000:13000 -p 12000:12000 -it ssv_node make BUILD_PATH=/go/bin/ssvnode start-node && docker logs ssv_node --follow
```