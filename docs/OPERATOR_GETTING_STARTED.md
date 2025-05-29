[<img src="./resources/ssv_header_image.png" >](https://www.ssvlabs.io/)

<br>
<br>

# SSV - Operator Getting Started Guide

- [Setting AWS Server for Operator](#setting-aws-server-for-operator)
  - [1. Setup](#1-setup)
  - [2. Login with SSH](#2-login-with-ssh)
  - [3. Installation Script](#3-installation-script)
  - [4. Generate Operator Keys](#4-generate-operator-keys)
  - [5. Create a Configuration File](#5-create-a-configuration-file)
    - [5.1 Logger Configuration](#51-logger-configuration)
    - [5.2 Metrics Configuration](#52-metrics-configuration)
    - [5.3 Profiling Configuration](#53-profiling-configuration)
  - [6. Start SSV Node in Docker](#6-start-ssv-node-in-docker)
  - [7. Update SSV Node Image](#7-update-ssv-node-image)
  - [8. Setup Monitoring](#8-setup-monitoring)

## Setting AWS Server for Operator

This section details the steps to run an operator on AWS.

### 1. Setup

Create a server of your choice and expose it on ports 12001 UDP and 13001 TCP

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

Mac\Linux:

```
$ cd ./{path to the folder to which the key pair file was downloaded}

$ chmod 400 {key pair file name}

$ ssh -i {key pair file name} ubuntu@{instance public IP}

type yes when prompted
```

Windows:

```
cd\{path to the folder to which the key pair file was downloaded}

ssh -i {key pair file name} ubuntu@{instance public IP}

type yes when prompted
```

### 3. Installation Script

Download and run the installation script.

```
$ sudo su

$ wget https://raw.githubusercontent.com/ssvlabs/ssv/main/install.sh

$ chmod +x install.sh

$ ./install.sh
```

### 4. Generate Operator Keys

The following command will generate your operator's public and private keys (appear as "pk" and "sk" in the output).

```
$ docker run --rm -it 'ssvlabs/ssv-node:latest' /go/bin/ssvnode generate-operator-keys
```

### 5. Create a Configuration File

Fill all the placeholders (e.g. `<ETH 2.0 node>` or `<db folder>`) with actual values,
and run the command below to create a `config.yaml` file.

```
$ yq n db.Path "<db folder>" | tee config.yaml \
  && yq w -i config.yaml eth2.BeaconNodeAddr "<ETH 2.0 node>" \
  && yq w -i config.yaml eth1.ETH1Addr "<ETH1 node WebSocket address>" \
  && yq w -i config.yaml OperatorPrivateKey "<private key of the operator>"
```

Example:

```yaml
db:
  Path: ./data/db/node_1
eth2:
  Network: prater
  BeaconNodeAddr: prater-4000-ext.stage.bloxinfra.com:80
eth1:
  ETH1Addr: ws://eth1-ws-ext.stage.bloxinfra.com/ws
OperatorPrivateKey: LS0tLS...
```

#### 5.1 Logger Configuration

In order to see `debug` level logs, add the corresponding section to the `config.yaml` by running:

```
$ yq w -i config.yaml global.LogLevel "debug"
```

Logs can be formatted as `json` instead of the default `console` format:

```
$ yq w -i config.yaml global.LogFormat "json"
```

Log levels can be shown in lowercase and w/o colors, default is upper case with colors:

```
$ yq w -i config.yaml global.LogLevelFormat "lowercase"
```

#### 5.2 Metrics Configuration

In order to enable metrics, the corresponding config should be in place:

```
$ yq w -i config.yaml MetricsAPIPort "15000"
```

See [setup monitoring](#8-setup-monitoring) for more details.

#### 5.3 Profiling Configuration

In order to enable go profiling tools, turn on the corresponding flag:

```
$ yq w -i config.yaml EnableProfile "true"
```

### 6. Start SSV Node in Docker

Before start, make sure the clock is synced with NTP servers.
Then, run the docker image in the same folder you created the `config.yaml`:

```shell
$ docker run -d --restart unless-stopped --name=ssv_node -e CONFIG_PATH=./config.yaml -p 13001:13001 -p 12001:12001/udp -v $(pwd)/config.yaml:/config.yaml -v $(pwd):/data --log-opt max-size=500m --log-opt max-file=10 -it 'ssvlabs/ssv-node:latest' make BUILD_PATH=/go/bin/ssvnode start-node \
  && docker logs ssv_node --follow
```

### 7. Update SSV Node Image

The current version is available through logs or a cmd:

```shell
$ docker run --rm -it 'ssvlabs/ssv-node:latest' /go/bin/ssvnode version
```

In order to update, kill running container and pull the latest image or a specific version (`ssvlabs/ssv-node:<version>`)

```shell
$ docker rm -f ssv_node && docker pull ssvlabs/ssv-node:latest
```

Now run the container again as specified above in step 6.
