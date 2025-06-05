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
    MsgWorkersCount: 2048 # default is 1024
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
