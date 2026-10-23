[<img src="../docs/resources/ssv_header_image.png" >](https://www.ssvlabs.io/)

<br>
<br>

# SSV - Exporter

## Overview

Exporter (AKA Fullnode) node is responsible for collecting and exporting information from the network to external systems such as the [Explorer](https://explorer.shifu.ssv.network/).

## Usage

In order to run an exporter, add the following in the config.yaml:
```yaml
p2p:
  Subnets: 0xffffffffffffffffffffffffffffffff
  MaxPeers: 150 # recommended but not a must
WebSocketAPIPort: 15000
```

In case the node misses messages, you can increase the amount of goroutines that handles incoming messages by:

```yaml
ssv:
  ValidatorOptions:
    MsgWorkersCount: 2048 # default is 256
```

With environment variables:
```dotenv
SUBNETS=0xffffffffffffffffffffffffffffffff
WS_API_PORT=15000
P2P_MAX_PEERS=150 # recommended but not a must
```

## APIs

Exporter Node provides WebSocket endpoints for reading the collected data. \
There are 2 types of end-points:

- `stream` - exporter pushes live data (decided messages) to the active sockets
- `query` - initiated by the consumer on demand to fetch historical data

### Message Structure

Request holds a `filter` for making queries of specific data 
and a `type` to distinguish between messages:
```
{
  "type": "decided"
  "filter": {
    "from": number,
    "to": number,
    "role": "ATTESTER" | "AGGREGATOR" | "PROPOSER",
    "publicKey": string
  }
}
```

Response extends the Request with a `data` section that contains the corresponding results:
```
{
  "type": "decided"
  "filter": {
    "from": number,
    "to": number,
    "role": "ATTESTER" | "AGGREGATOR" | "PROPOSER",
    "publicKey": string
  }
  "data": DecidedMessage[]
}
```

In addition, response might reflect an error, see [Error Handling](#error-handling):
```
{
  "type": "error",
  "data": string[]
}
```

### Data Model

The following objects will be used by the API:

#### Decided Messages

  ```json
  {
    "message": {
      "type": 3,
      "round": 1,
      "identifier": "...",
      "seq_number": 23,
      "value": "..."
    },
    "signature": "...",
    "signer_ids": [2, 1, 3]
  }
  ```

### End Points

#### Stream

`/stream` is an API that allows consumers to get live data that is collected by the exporter, which will push the information it receives from SSV nodes.

For example, exporter will push a message for a new decided message that was broadcast-ed in the network:
```json
{
  "type": "decided",
  "filter": {"publicKey": "...", "role": "ATTESTER", "from": 2341, "to": 2341 },
  "data": [{ ... }]
}
```

#### Query

`/query` is an API that allows some consumers to request data, by specifying filter.

Example request:
```json
{ "type": "decided", "filter": { "publicKey": "...", "role": "ATTESTER", "from": 2, "to": 4 } }
```

The corresponding response:
```json
{ "type": "decided", "filter": { "publicKey": "...", "role": "ATTESTER", "from": 2, "to": 4 }, "data":[...] }
```

##### Error Handling

In case of bad request or some internal error, the response will be of `type` "error".

Some examples:

- Bad input (corrupted JSON) produces:
  ```json
  {
    "type": "error",
    "filter": {
      "from": 0,
      "to": 0
    },
    "data": [
      "could not parse network message"
    ]
  }
  ```
- Unknown message type results:
  ```json
  {
    "type": "error",
    "filter": {
      "from": 0,
      "to": 0
    },
    "data": [
      "bad request - unknown message type 'foo'"
    ]
  }
  ```


### Glamsterdam (Gloas) duties

From the Gloas fork the exporter records two more validator duties, under their own roles in `/v1/exporter/traces/validator` and `/v1/exporter/decideds`:

- `PTC_ATTESTER` — the payload-timeliness attestation (SIP #94 §3): one partial-signature round, no consensus.
- `PROPOSER_PREFERENCES` — the proposer's preferences for an upcoming proposal (SIP #94 §5), recorded under the proposal slot even though operators broadcast them up to two epochs early. In archive mode the trace also holds the builder request-auth signatures (`type: REQUEST_AUTH`), which do not count as participation.

Partial-signature entries carry a `type` (`POST_CONSENSUS`, `PTC_ATTESTER`, `PROPOSER_PREFERENCES`, `REQUEST_AUTH`, ...). A validator duty's trace holds one entry per root each signer signs, timed at its first sighting, so a packet with several roots yields several entries: the block and §6 envelope roots of a self-build proposer's post-consensus packet at a Gloas slot, one per subnet for a sync-committee contribution, one per builder for a request-auth packet. At Gloas slots a proposer trace's `proposalData` is the decided `GloasProposalData` (block plus `payload_root`) rather than a versioned block.

In archive mode, `signers` in `/v1/exporter/decideds` lists the operators seen signing rather than a quorum, as it always has for post-consensus signers: the envelope root's signers count toward `PROPOSER`, request auth never counts, and a `PROPOSER_PREFERENCES` duty whose trace holds request auth alone reports no entry. Standard mode records a duty's participants only once a signing root reaches the committee's quorum, and for `PROPOSER` only the block root's quorum counts.

The trace endpoints' schedules list the two roles like `PROPOSER`: `PTC_ATTESTER` for the slot's PTC members and, at Gloas slots, `PROPOSER_PREFERENCES` for its proposers. `/v1/exporter/traces/committee` reports every scheduled role of a committee's validators, these two included.

In standard mode the `/stream` WebSocket pushes decided messages for the two roles as well. A `PROPOSER_PREFERENCES` message carries its proposal slot, so it can arrive up to two epochs before that slot.

A `PROPOSER_PREFERENCES` trace is written to disk only when its proposal slot is evicted, up to two epochs after its messages, so an exporter restarted in between loses it; every other role loses at most four slots.

Traces written by this version can hold more than 13 entries in a duty's `pre` or `post` (a self-build proposer's two roots signed by 7 or more operators already do), which an older exporter can't read. The store records its format version, and a store written by a newer version is refused at startup rather than read partially.

### Explore API

Use a tool for WebSockets (such as [wscat](https://www.npmjs.com/package/wscat)) to interact with the API.

```shell
wscat -c ws://ws-exporter.stage.ssv.network/query
```

Once connection is ready, type your query:

```shell
> { "type": "decided", "filter": { "publicKey": "...", "role": "ATTESTER", "from": 2, "to": 4 } }
```

The expected results contain a list of desired decided messages, in our case in index `[2, 4]`
```shell
< { "type": "decided", "filter": { "publicKey": "...", "role": "ATTESTER", "from": 2, "to": 4 }, "data":[...] }
```
