# SSV - Roadmap

This document describes the current status and the upcoming milestones of the SSV node project.

_Updated: Tue, 28 Feb 2023_

## SSV

### Milestone Summary

| Status | Milestone                                                         | Goals  |
| :----: | :---------------------------------------------------------------- | :----: |
|   🚀   | **[IBFT Based Consensus](#ibft-based-consensus)**                 | 3 / 3  |
|   🚀   | **[SSV Spec Alignment](#ssv-spec-alignment)**                     | 3 / 6  |
|   🚀   | **[SSV Node Infrastructure](#ssv-node-infrastructure)**           | 5 / 14 |
|   🚀   | **[Ethereum Spec Implementation](#ethereum-spec-implementation)** | 5 / 8  |
|   🚀   | **[Network & Discovery](#network--discovery)**                    | 5 / 9  |
|   🚀   | **[Validator Management](#validator-management)**                 | 4 / 6  |
|   🚀   | **[Monitoring & Tools](#monitoring--tools)**                      | 3 / 4  |
|   🚀   | **[Testnets](#testnets)**                                         | 4 / 5  |

### IBFT Based Consensus

⭐ &nbsp;**CLOSED** &nbsp;&nbsp;📉 &nbsp;&nbsp;**3 / 3** goals completed **(100%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                             |
| :----: | :------------------------------- |
|   ✔    | iBFT Consensus Go Implementation |
|   ✔    | SSV Specific iBFT Implementor    |
|   ✔    | Port POC Code To Golang          |

## SSV Spec Alignment

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**3 / 6** goals completed **(50%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                                                               |
| :----: | :----------------------------------------------------------------- |
|   ✔    | [SSV Spec v0.2.6](https://github.com/ssvlabs/ssv-spec/tree/V0.2.6) |
|   ✔    | [SSV Spec v0.2.7](https://github.com/ssvlabs/ssv-spec/tree/V0.2.7) |
|   ✔    | [SSV Spec v0.2.8](https://github.com/ssvlabs/ssv-spec/tree/V0.2.8) |
|   ❌   | [SSV Spec v0.2.9](https://github.com/ssvlabs/ssv-spec/tree/V0.2.9) |
|   ❌   | [SSV Spec v0.3.0]()                                                |
|   ❌   | [Post Audit SSV Spec]()                                            |

## SSV Node Infrastructure

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**5 / 14** goals completed **(35%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                                                                                          |
| :----: | :-------------------------------------------------------------------------------------------- |
|   ✔    | Storage Integration And Recovery (Sync)                                                       |
|   ✔    | Between Instance Persistence (Prevent Starting A New Instance If Previous Not Decided)        |
|   ✔    | Full Node(Archive) & Light Node Support                                                       |
|   ✔    | Pass Spec Test                                                                                |
|   ✔    | Deployment                                                                                    |
|   🚧   | Documentation                                                                                 |
|   ❌   | SSV Fork Support                                                                              |
|   🚧   | Replace Prysm Dependency With [go-eth2-client](https://github.com/attestantio/go-eth2-client) |
|   🚧   | Integration Tests Implementation                                                              |
|   🚧   | Refactor Logs                                                                                 |
|   🚧   | V3 Contract Integration                                                                       |
|   🚧   | SSZ Support                                                                                   |
|   ❌   | Optimize ETH1 Sync & Management Of Events                                                     |
|   ❌   | Audit                                                                                         |

## Ethereum Spec Implementation

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**5 / 8** goals completed **(62%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                                                    |
| :----: | :------------------------------------------------------ |
|   ✔    | Prysm Beacon Node Support(GRPC)                         |
|   ✔    | Multi Beacon Node Implementation Support (Standard API) |
|   ✔    | Aggregation Support                                     |
|   ✔    | Proposal Support                                        |
|   ✔    | Sync Committee Support                                  |
|   🚧   | Beacon Node Fork Support                                |
|   🚧   | Cappella Fork Support                                   |
|   🚧   | MEV Support                                             |

## Network & Discovery

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**5 / 9** goals completed **(55%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                                                              |
| :----: | :---------------------------------------------------------------- |
|   ✔    | Integrate libp2p & Disc V5                                        |
|   ✔    | Network Topology Based On Validators (Subnet Per Validator)       |
|   ✔    | SSV Cluster Support (Multiple Validators Per Cluster)             |
|   ✔    | Multi Cluster Support (Operator Can Be Part Of Multiple Clusters) |
|   ✔    | Peer Scoring & Peer Management                                    |
|   🚧   | Message Validation On Network Layer                               |
|   ❌   | 10k Validators Support                                            |
|   ❌   | Scale Tests                                                       |
|   ❌   | Attack tests                                                      |

## Validator Management

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**4 / 6** goals completed **(66%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                                                             |
| :----: | :--------------------------------------------------------------- |
|   ✔    | Validator Key Sharing                                            |
|   ✔    | Validator Share Signer - [EKM]()                                 |
|   ✔    | Slashing Protection                                              |
|   ✔    | Support 7,10 & 13 shares                                         |
|   ❌   | Remote signer [EIP3030](https://eips.ethereum.org/EIPS/eip-3030) |
|   ❌   | DKG                                                              |

## Monitoring & Tools

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**3 / 4** goals completed **(75%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                                              |
| :----: | :------------------------------------------------ |
|   ✔    | Prometheus and Grafana support                    |
|   ✔    | Read Only mode (Exporter)                         |
|   ✔    | V2 Grafana Dashboards (Node health & Performance) |
|   🚧   | Exporter Support Multi Duties                     |

## Testnets

🚀 &nbsp;**OPEN** &nbsp;&nbsp;📉 &nbsp;&nbsp;**4 / 5** goals completed **(80%)** &nbsp;&nbsp;📅 &nbsp;&nbsp;**Feb 28 2023**

| Status | Goal                            |
| :----: | :------------------------------ |
|   ✔    | Private testnet                 |
|   ✔    | Primus first public testnet     |
|   ✔    | Shifu testnet (V2 contracts)    |
|   ✔    | Shifu V2 testnet (Multi duties) |
|   🚧   | V3 Testnet(TBD) - RC Candidate  |
