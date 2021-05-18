[<img src="../img/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# Secret-Shared-Validator(SSV)
Secret Shared Validator ('SSV') is a unique technology that enables the distributed control and operation of an Ethereum validator.\
SSV uses an MPC threshold scheme with a consensus layer on top, that governs the network. Its core strength is in its robustness and\
fault tolerance which leads the way for an open network of staking operators to run validators in a decentralized and trustless way. 

## SSV Operator
An operator is an entity running the SSV node code (found here) and joining the SSV network.

### General SSV information (Semi technical read)
* Article by [Blox](https://medium.com/bloxstaking/an-introduction-to-secret-shared-validators-ssv-for-ethereum-2-0-faf49efcabee)
* Article by [Mara Schmiedt and Collin Mayers](https://medium.com/coinmonks/eth2-secret-shared-validators-85824df8cbc0)

### Technical iBFT and SSV read
* [iBFT Paper](https://arxiv.org/pdf/2002.03613.pdf)
* [iBFT annotated paper (By Blox)](https://docs.google.com/document/d/1aIJVw92k4I3p5SM3Qarp0AvxJo70ZdM0s5a1arKgVGg/edit?usp=sharing)

## Running a local network
1. Clone the github repo
```bash
$ git clone https://github.com/ethereum/eth2-ssv.git
```
2. Split validator key into 4 shares (can be more than 4 shares but at the moment it’s hard coded)
```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
$ ./bin/ssvnode export-keys --mnemonic={mnemonic} --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count {# of ssv nodes} --private-key {privateKey}
```
3. Create 4 .env files with the names .env.node.1, .env.node.2, .env.node.3, .env.node.4
4. Use the template .env file for step #3
```bash
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
5. place the 4 .env files in the same folder as the eth2-SSV project
6. Run a local network
```bash
# Build binary
$ CGO_ENABLED=1 go build -o ./bin/ssvnode ./cmd/ssvnode/

# Run local 4 node network (requires docker and a .env file as shown below)
$ make docker-debug 
```
