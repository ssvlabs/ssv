[<img src="./internals/img/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Secret Shared Validator

SSV is a protocol for distribuiting an eth2 validator key between multiple operators governed by a consensus protocol (Istanbul BFT).

### TODO
[\\] Free standing, reference iBFT Go implementation\
[\\] SSV specific iBFT implementor\
[\\] Port POC code to Glang\
[ ] Single standing instance running with Prysm's validator client\
[ ] Networking and discovery\
[\\] db, persistance and recovery\
[ ] Multi network support (being part of multiple SSV groups)\
[X] Key sharing\
[ ] Documentation\
[ ] Phase 1 changes\
[ ] Audit

** X=done, \\=WIP


### Research
- Secret Shared Validators on Eth2
    - [Litepaper](https://medium.com/coinmonks/eth2-secret-shared-validators-85824df8cbc0)
- iBTF
    - [Paper](https://arxiv.org/pdf/2002.03613.pdf)
    - [EIP650](https://github.com/ethereum/EIPs/issues/650)
    - [Liveness issues](https://github.com/ConsenSys/quorum/issues/305) - should have been addressed in the paper
    - [Consensys short description](https://docs.goquorum.consensys.net/en/stable/Concepts/Consensus/IBFT/)
- POC
    - [SSV Python node](https://github.com/dankrad/python-ssv)
    - [iBFT Python](https://github.com/dankrad/python-ibft)
    - [Prysm adapted validator client](https://github.com/alonmuroch/prysm/tree/ssv)
- Other implementations
    - [Consensys Quorum](https://github.com/ConsenSys/quorum)   
    - [Besu Hyperledger](https://besu.hyperledger.org/en/stable/HowTo/Configure/Consensus-Protocols/IBFT/)
        - [code]( https://github.com/hyperledger/besu/tree/master/consensus/ibft)
- DKG
    - [Blox's eth2 pools research](https://github.com/bloxapp/eth2-staking-pools-research)
    - [ETH DKG](https://github.com/PhilippSchindler/ethdkg)


# Getting started
### Build
```bash
# Build binary
$ CGO_ENABLED=1 go build -o ./bin/ssvnode ./cmd/ssvnode/

# Run tool
$ ./bin/ssvnode --help

# Run node
$ make NODE_ID=1  BUILD_PATH="./bin/ssvnode"  start-node

```
    
### Preparation
##### Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
- Private keys from mnemonic: ` ./bin/ssvnode export-keys --mnemonic={mnemonic} --index={keyIndex}`

##### Generate threshold keys
- `./bin/ssvnode create-threshold --count {# of ssv nodes} --private-key {privateKey}`
   
##### Edit config files
- .env
```
   NETWORK=pyrmont/mainnet
   BEACON_NODE_ADDR
   VALIDATOR_PUBLIC_KEY
   SSV_NODE_{index} - for each threshold created add param(public key {index} from previous step )
```
- docker-compose.yaml

  ` PUBKEY_NODE_{index} - add the other public keys as environment for each service`    

### How to run

`docker-compose.yaml` contains definitions for 2 sets (prod & debug) of 4 SSV nodes with its own threshold private keys that are generated based on the 
validator's private key. All needed parameters can be found in `docker-compose.yaml` and `.env` files.


```bash 
# Run 4 nodes (prod mode)
$ make docker

# Run 4 nodes (debug & live reload mode) 
$ make docker-debug
```    

### Contribute
#### Debug(TODO)
#### Lint
```bash 
# install linter
$ make lint-prepare

# Run nodes
$ make lint
```

# Phase 1 Testnet deployment  ![ethereum](/github/resources/ethereum.gif)

#### Server Preparation
##### Create a server of your choice and expose on ports 12000 TCP and 13000 UDP
(AWS example below)
- In the search bar search for "ec2"
- Launch new instance
- choose "ubuntu server 20.04"
- choose "t2.micro" (free tire)
- skip to "security group" section
- make sure you have 3 rules. UDP, TCP and SSH -
![security_permission](/github/resources/security_permission.png)
- when promote, add new key pair and download the ssh file 
- launch instance

##### SHH permissions and login to server-  
```
$ cd ./{path to where the ssh downloaded}

$ chmod 400 {ssh file name}

$ ssh -i {ssh file name} ubuntu@{server public ip}

$ sudo su
```

#### .env file
 
 - Export all required params
    * If you'r node 1, need to fill the other nodes (2,3,4) and so on...     
```
$ touch .env

$ echo "CONSENSUS_TYPE=validation" >> .env
$ echo "NETWORK=pyrmont" >> .env
$ echo "BEACON_NODE_ADDR={ETH 2.0 node}" >> .env
$ echo "VALIDATOR_PUBLIC_KEY={validator public key}" >> .env
$ echo "NODE_ID={provided node index}" >> .env
$ echo "SSV_PRIVATE_KEY={provided node private key}" >> .env
$ echo "PUBKEY_NODE_1={provided node index 1 public key}" >> .env
$ echo "PUBKEY_NODE_2={provided node index 2 public key}" >> .env
$ echo "PUBKEY_NODE_3={provided node index 3 public key}" >> .env 
$ echo "PUBKEY_NODE_4={provided node index 4 public key}" >> .env
```

##### Download and run install.sh script 
```
$ wget https://raw.githubusercontent.com/bloxapp/ssv/stage/install.sh

$ chmod +x install.sh

$ ./install.sh
```

- Install script result with docker container id

- You can watch logs using that cmd - 
```
$ docker logs ssv_node --follow
``` 