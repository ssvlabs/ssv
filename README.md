[<img src="./internals/img/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Secret Shared Validator

SSV is a protocol for distribuiting an eth2 validator key between multiple operators governed by a consensus protocol (Istanbul BFT).

### TODO
[\] Free standing, reference iBFT Go implementation
[\] SSV specific iBFT implementor
[ ] Port POC code to Glang\
[ ] Single standing instance running with Prysm's validator client\
[ ] Networking and discovery\
[ ] db, persistance and recovery\
[ ] Multi network support (being part of multiple SSV groups)\
[X] Key sharing\
[ ] Documentation\
[ ] Phase 1 changes\
[ ] Audit


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

     
# How to run

```bash 
$ docker-compose build ssv
$ docker-compose up -d ssv
```

The following commands are for building and running CLI to generate threshold keys:
```bash
# Build binary
$ CGO_ENABLED=1 go build ./cmd/ssvcli/

# Run tool
$ ./ssvcli create-threshold --key <hex-encoded-private-key>
```