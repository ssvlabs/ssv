[<img src="./internals/img/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Secret Shared Validator

SSV is a protocol for distributing an eth2 validator key between multiple operators governed by a consensus protocol ([Istanbul BFT](https://arxiv.org/pdf/2002.03613.pdf)).


## ⚠️ Phase 1 testing ⚠️
Please see [phase 1 testing doc](./PHASE_1_TESTNET.md) for details


## Getting started
```bash
# Build binary
$ CGO_ENABLED=1 go build -o ./bin/ssvnode ./cmd/ssvnode/

# Run local 4 node network (requires docker and a .env file as shown below)
$ make docker-debug 

# Lint
$ make lint-prepare

$ make lint

# Full test
$ make full-test

```

## Splitting a key
We split an eth2 BLS validator key into shares via Shamir-Secret-Sharing(SSS) to be used between the SSV nodes. 
```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
$ ./bin/ssvnode export-keys --mnemonic={mnemonic} --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count {# of ssv nodes} --private-key {privateKey}
```

## Example .env file 
```
   DISCOVERY_TYPE=mdns
   CONSENSUS_TYPE=validation
   NETWORK=pyrmont
   BEACON_NODE_ADDR=eth2-4000-prysm.stage.bloxinfra.com:80
   VALIDATOR_PUBLIC_KEY=
   VALIDATOR_PRIVATE_KEY= # NOT USED FOR NOW
   SSV_NODE_1=
   SSV_NODE_PUB_KEY_1=
   SSV_NODE_2=
   SSV_NODE_PUB_KEY_2=
   SSV_NODE_3=
   SSV_NODE_PUB_KEY_3=
   SSV_NODE_4=
   SSV_NODE_PUB_KEY_4=
```

### Progress
[X] Free standing, reference iBFT Go implementation\
[X] SSV specific iBFT implementor\
[X] Port POC code to Glang\
[ ] Single standing instance running with Prysm's validator client\
[\\] Networking and discovery\
[\\] db, persistance and recovery\
[ ] Between instance persistence (pevent starting a new instance if previous not decided)
[ ] Multi network support (being part of multiple SSV groups)\
[X] Key sharing\
[X] Deployment\
[\\] Documentation\
[ ] Phase 1 changes\
[ ] Audit

** X=done, \\=WIP


### Research (Deprecated)
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