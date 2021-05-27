# Exporter Node

## Intro

Exporter node is responsible for exposing data from SSV Network to the Explorer Center, which index the data and provides an API for the Web UI.

The Web UI shows information for a validator,
It provides a way for validator to inspect the operators' performance, duties history and more.

### Links

* [BLOXSSV-157](https://bloxxx.atlassian.net/browse/BLOXSSV-157)

## Design

Exporter node is new type of peer that needs to pull and store data from SSV nodes or smart contract. \
As most of that logic already exist in SSV, the exporter is just a new executable, re-using existing code from SSV and have slightly different configuration.

<img src="./resources/exporter-node-diagram.png" >

### Data

The following information will be needed:
* Operators 
  * Name --> contract
  * Public Key --> contract
  * Performance --> contract (score) ?
    * won't be needed as the explorer center calculates it
* Validators
  * Public Key --> contract
  * Owner Address --> contract
  * Payment Address --> contract
  * Assigned Operators (over time) --> contract (oess)
* Duties (over time) 
  * Epoch --> calculated from Slot
  * Slot --> part of the lambda
  * Duty type / role --> part of the lambda
  * Status (failed | success)
  * Operators --> signer_ids + id lookup in contract information (oess > index)
    * operators with the corresponding indication for each operator on the duty

The following diagram shows the initial mockup of Web UI:

<img src="./resources/web-explorer-screenshot.png" >

#### Contract Data

Events to listen:
* `OperatorAdded`
* `ValidatorAdded`
* `OessAdded`

##### Sync

In order to have all the needed data, exporter needs to [read all events logs](https://goethereumbook.org/event-read/) 
of the specified events. 

[`FilterLogs()`](https://github.com/ethereum/go-ethereum/blob/master/ethclient/ethclient.go#L387) 
accepts [`FilterQuery`](https://github.com/ethereum/go-ethereum/blob/master/interfaces.go#L138) 
that enables to query a specific contract by addresses, and to provide a scope of blocks 
(`FromBlock`, `ToBlock`)

A genesis block for the contract can be used as a baseline block (the block to start the sync)

#### IBFT Data

Interaction with SSV nodes can be done using the existing sync end-point (`libp2p` stream)
  
### Persistency

A storage for Exporter Node should support persistence of:
* Operators
* Validators
* IBFT (decided)

#### Database

A Key-Value Store (e.g. Badger) is a good candidate because the Explorer Center does the indexing with ES. 

Badger is an embedded DB (stored in FS), therefore won't support HA. \
In order to achieve HA, one of the following should be the way to go:
* Use some other remote DB (e.g. S3)
* Use a shared volume (K8S)

### APIs

#### Explorer Center

Exporter Node will provide the following end-points:

  ##### Get all operators
  
  `GET` `/operators`
  
  ```json
  {
    "data": [],
    "timestamp": 1622361865789
  }
  ```
  
  ##### Get all validators
  
  `GET` `/validators`
  
  ```json
  {
    "data": [],
    "timestamp": 1622361865789
  }
  ```


  ##### Get ibft data 
  
  Input: slot + seq_number
  
  * consider streams