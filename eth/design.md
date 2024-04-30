# [Execution Client - Information Retrival] Design

## :beginner: Info

:small_blue_diamond:Author: y0sher, Moshe, Nikita

:small_blue_diamond:Status: Ready for review

## :abc: Overview

:small_blue_diamond:Based on - [Execution Client MindMap](https://hackmd.io/qjDKMEEaQrORwNL7YGKSiQ)

Execution layer communication is an important part of SSV, we are basing our state and actions on the SSV contract events.

## :feet: Goals

- Alawys be up-top-date with synced-el-node that reads the contract events.
- Stop the node if el-node is out of sync.
- Atomic event handling - support crash recovery, interlock actions on validators with live incoming events.
- Simple flow between history and ongiong sync.
- Improve ongoingSync - update SyncOffset live, remove need to save and skip events.
- Integrity:
  - Atomicity: Event handlers must contain all of their database writes in a single transaction, only committing it after the event was handled successfully.
    > This principle ensures that every transaction within an event handler is indivisible, meaning all changes are committed simultaneously or none at all. This mechanism protects against partial data updates and system inconsistencies.
  - Consistency: All contract events are processed without exception. Should a non-deterministic error occur (e.g., temporary network or hardware issues), the system must fail and cause the node to crash. Upon node restart, the event that encountered an error will be reprocessed, ensuring no event is missed in the syncing process. Execution client syncing would not continue past this event until it has been successfully processed.
- Re-org handling, be able to provide up-top-date data even when re-orgs happen

## :wrench: Design Components

- `ExecutionClient` - A wrapper around Geth api client, used to communicate with the API only.
- `EventSyncer` - Reads events from `ExecutionClient` and processes them using `EventHandler`.
- `EventHandler` - Handles the data aspects of the events, process each block as a DB transaction. has access to database. can shoot the event data or process data again after finishing successfull tx. this component also updates what we know today as "SyncOffset", can be named "LastProcessedBlock".
- `operator/node` - Glues together options to create the `ExecutionClient`, `EventSyncer` and `EventHandler`.
- `operator/controller` use of successfully handeled data events (`EventHandler`) to execute tasks (validator start/stop, etc)

## :question: APIs, suggestions and examples

### Resources

- [Flow chart](https://pasteboard.co/rmaBLhc78cfB.png)
- [Contract event list](https://docs.google.com/spreadsheets/d/1I8buKkZxYgrlSlL4MbXUGULvUsNZj50MQMDji1cnA7o/edit#gid=0)

### Reorg handling

Currently we have decided to only read events up to finalized checkpoint blocks. We might need to ask eth2 node for this block number/hash but this can be done at `operator/node` and passed into the `EventBatcher` for separation.

### might be interesting -

- [log-flume](https://github.com/openrelayxyz/log-flume/blob/master/flumeserver/datafeed/websockets.go#L49)
- safeethclient
- [logs scanner with gap missing](https://github.com/hydroscan/hydroscan-api/blob/master/task/event_subscriber.go)

### Contract ABI

Use contract Binding for parsing event, generated code will mean no need to update on ABI change. (only generate).
ex: https://geth.ethereum.org/docs/developers/dapp-developer/native-bindings

### Pseudocode for atomic block processing

```golang
for {
    select {
    case blockWithEvents := <-blocks:
        tx := db.Begin()
        tasks := processBlock(blockWithEvents)
        UpdateSyncOffset()
        tx.Commit()
        tasksChannel <- tasks
    case ctx.Done():
        return
    }
}
```

### EL sync

if the EL node is not synced, we should stop event processing and signal the node to crash becasue we can't resume.
a synecd EL node is a prerequisite to run SSV.

After crash the SSV node will launch and will wait and retry until it synced, so it won't be in a crash loop.

### Observability

- Event processing time
- malformed events
- last processesd block
