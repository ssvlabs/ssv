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

Create a server of your choice and expose it on ports 12000 UDP and 13000 TCP
- In the search bar search for "ec2" and then click on EC2 in the search results
- In the EC2 Dashboard, select Launch Instance
- Select "Ubuntu Server 20.04"
- Choose "t2.micro" (free tier, should be selected by default)
- Go to the "Configure Security Group" tab at the top
- Make sure you have 3 rules (use the Add Rule button as necessary) - Custom UDP, Custom TCP and SSH, and make sure to set their Port Range and Source attributes as seen in the screenshot below -
![security_permission](./resources/security_permission.png)
- Click on "Review and Launch" and then "Launch"
- In the key pair pop-up, select "Create a new key pair" in the drop-down, then name this key pair and download it
- Click Launch Instances and then View Instances
- In the instances table, take note of the Public IP of your newly created instance

### 2. Login with SSH

```
$ cd ./{path to the folder to which the key pair file was downloaded}

$ chmod 400 {key pair file name}

$ ssh -i {key pair file name} ubuntu@{instance public IP}

type yes when prompted
```

### 3. Installation Script

Download and run the installation script.

```
$ sudo su

$ wget https://raw.githubusercontent.com/ethereum/eth2-ssv/stage/install.sh

$ chmod +x install.sh

$ ./install.sh
```

### 4. Generate Operator Keys

The following command will generate your operator's public and private keys (appear as "pk" and "sk" in the output). 

```
$ docker run -d --name=ssv_node_op_key -it 'bloxstaking/ssv-node:latest' \
/go/bin/ssvnode generate-operator-keys && docker logs ssv_node_op_key --follow \
&& docker stop ssv_node_op_key && docker rm ssv_node_op_key
```

### 5. Create a Configuration File

Fill all the placeholders (e.g. `<ETH 2.0 node>` or `<db folder>`) with actual values,
and run the command below to create a `config.yaml` file.


```
$ yq n db.Path "<db folder>" | tee config.yaml \
  && yq w -i config.yaml db.Type "badger-db" \
  && yq w -i config.yaml Network "prater" \
  && yq w -i config.yaml DiscoveryType "discv5" \
  && yq w -i config.yaml BeaconNodeAddr "<ETH 2.0 node>" \
  && yq w -i config.yaml ETH1Addr "<ETH1 node>" \
  && yq w -i config.yaml OperatorPrivateKey "<private key of the operator>" \
  && yq w -i config.yaml SmartContractAddr "0x9573c41f0ed8b72f3bd6a9ba6e3e15426a0aa65b"
```

`config.yaml` example:

```yaml
db:
  Path: ./data/db/node_1
  Type: badger-db

Network: prater
DiscoveryType: discv5
BeaconNodeAddr: prater-4000-ext.stage.bloxinfra.com:80
ETH1Addr: ws://eth1-ws-ext.stage.bloxinfra.com/ws
```

  #### 5.1 Debug Configuration

  In order to see `debug` level logs, add the corresponding section to the `config.yaml` by running:

  ```
$ yq w -i config.yaml global.LogLevel "debug"
  ```


### 6. Start Node in Docker

Run the docker image in the same folder you created the `config.yaml`:

```
$ docker run -d --restart unless-stopped --name=ssv_node -e CONFIG_PATH=./config.yaml -p 13000:13000 -p 12000:12000 -v $(pwd)/config.yaml:/config.yaml -v $(pwd):/data -it 'bloxstaking/ssv-node:latest' make BUILD_PATH=/go/bin/ssvnode start-node \
  && docker logs ssv_node --follow
```
