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

### 4. Create Operator Keys

The following command will create new keys:
```
$ docker run -d --name=ssv_node_op_key -it 'bloxstaking/ssv-node:latest' \
/go/bin/ssvnode generate-operator-keys && docker logs ssv_node_op_key --follow \
&& docker stop ssv_node_op_key && docker rm ssv_node_op_key
```

### 5. Create a Configuration File

Fill all the placeholders (e.g. `<ETH 2.0 node>` or `<privkey of the operator>`) with actual values,
and run the command below to create a `config.yaml` file.

```
$ yq e -n '.db = {"Path": "<db folder>", "Type": "badger-db"}' \
  | yq e '.Network = "pyrmont"' - \
  | yq e '.BeaconNodeAddr = "<ETH 2.0 node>"' - \
  | yq e '.ETH1Addr = "<ETH1 node>"' - \
  | yq e '.HostAddress = "<host public ip>"' - \
  | yq e '.OperatorKey = "<privkey of the operator>"' - \
  | tee config.yaml
```

`config.yaml` example:

```yaml
db:
  Path: ./data/db/node_1
  Type: badger-db

Network: pyrmont
BeaconNodeAddr: prysm.stage.blox.com:80
ETH1Addr: ws://eth1.stage.blox.com/ws
HostAddress: 84.220.13.151
```

### 6. Start Node in Docker

Run the docker image from the same folder containing `config.yaml`.
```
$ docker run -d --restart unless-stopped --name=ssv_node -e CONFIG_PATH=./config.yaml -p 13000:13000 -p 12000:12000 -v $(pwd)/config.yaml:/config.yaml -it 'bloxstaking/ssv-node:latest' make BUILD_PATH=/go/bin/ssvnode start-node && docker logs ssv_node --follow
```
