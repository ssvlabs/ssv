[<img src="./docs/resources/ssv_header_image.png" >](https://www.ssv.network/)

<br>
<br>

# SSV - Secret Shared Validator

[![API Reference](
https://camo.githubusercontent.com/915b7be44ada53c290eb157634330494ebe3e30a/68747470733a2f2f676f646f632e6f72672f6769746875622e636f6d2f676f6c616e672f6764646f3f7374617475732e737667
)](https://pkg.go.dev/github.com/ethereum/eth2-ssv?tab=doc)
![Github Actions](https://github.com/ethereum/eth2-ssv/actions/workflows/full-test.yml/badge.svg?branch=stage)
![Github Actions](https://github.com/ethereum/eth2-ssv/actions/workflows/lint.yml/badge.svg?branch=stage)
![Test Coverage](./docs/resources/cov-badge.svg)
[![Discord](https://img.shields.io/badge/discord-join%20chat-blue.svg)](https://discord.gg/ssvnetworkofficial)

[comment]: <> ([![Go Report Card]&#40;https://goreportcard.com/badge/github.com/ethereum/eth2-ssv&#41;]&#40;https://goreportcard.com/report/github.com/ethereum/eth2-ssv&#41;)

[comment]: <> ([![Travis]&#40;https://travis-ci.com/ethereum/eth2-ssv.svg?branch=stage&#41;]&#40;https://travis-ci.com/ethereum/eth2-ssv&#41;)

## Introduction

Secret Shared Validator ('SSV') is a unique technology that enables the distributed control and operation of an Ethereum validator.

SSV uses an MPC threshold scheme with a consensus layer on top ([Istanbul BFT](https://arxiv.org/pdf/2002.03613.pdf)), 
that governs the network. \
Its core strength is in its robustness and fault tolerance which leads the way for an open network of staking operators 
to run validators in a decentralized and trustless way.

## Information

### General SSV information (Semi technical read)
* Article by [ssv](https://blog.ssv.network/an-introduction-to-secret-shared-validators-ssv-for-ethereum-2-0-faf49efcabee)

### Technical iBFT and SSV read
* [iBFT Paper](https://arxiv.org/pdf/2002.03613.pdf)
    * [Fast sync for current instance](./ibft/sync/speedup/README.md)
* [iBFT annotated paper (By Blox)](./ibft/IBFT.md)
* [EIP650](https://github.com/ethereum/EIPs/issues/650)
* [Security proof for n-t honest parties](https://notes.ethereum.org/DYU-NrRBTxS3X0fu_MidnA)
* [MEV Research - Block proposer/ builder separation in SSV](https://hackmd.io/DHt98PC_S_60NbnW4Wgssg)

### Audits
- [Least Authority](/audits/Least%20Authority.pdf) (August 2023)

## Getting Started

The following documents contain instructions and information on how to get started:
* [Operator Node Installation](https://docs.ssv.network/run-a-node/operator-node/installation)
* [Developers' Guide](./docs/DEV_GUIDE.md)
* [SSV API Docs](https://ssvlabs.github.io/ssv/)

## Contribution

Thank you for considering a contribution to the source code.

In order to contribute to eth2-ssv, please fork, add your code, commit and send a pull request
for the maintainers to review and merge into the main code base.\
If you wish to submit more complex changes though, please check up with the core devs first on [our discord](https://discord.gg/eDXSP9R)
to ensure those changes are in line with the general philosophy of the project and/or get
some early feedback which can make both your efforts much lighter as well as our review
and merge procedures quick and simple.

Please see the [Developers' Guide](./docs/DEV_GUIDE.md)
for more details on configuring your environment, managing project dependencies, and
testing procedures.

## License

The eth2-ssv library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in our repository in the `LICENSE` file.

The eth2-ssv binaries (i.e. all code inside of the `cmd` directory) is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `LICENSE` file.

## Road Map

For detailed roadmap please check out [ROADMAP.md](./ROADMAP.md) 

