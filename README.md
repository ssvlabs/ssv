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
$ CGO_ENABLED=1 go build -o bin/ssv ./cmd/ssvnode/

# Run tool
$ ./bin/ssv --help

```
    
### Preparation
##### Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
- Private keys from mnemonic: ` ./bin/ssv export-keys --mnemonic={mnemonic} --index={keyIndex}`        

##### Generate threshold keys
- `./bin/ssv create-threshold --count {# of ssv nodes} --private-key {privateKey}`
   
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

`docker-compose.yaml` contains definitions of 3 SSV nodes with its own threshold private keys that are generated based on the 
validator's private key. All needed parameters can be found in `docker-compose.yaml` and `.env` files.



```bash 
# Build nodes
$ docker-compose build

# Run nodes
$ docker-compose up -d
```    