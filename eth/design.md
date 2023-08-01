# [ETH1 - Information Retrival] Design

## :beginner: Info

:small_blue_diamond:Author: y0sher, Moshe, Nikita

:small_blue_diamond:Status: Ready for review

## :abc: Overview

:small_blue_diamond:Based on - [ETH1 MindMap](https://hackmd.io/qjDKMEEaQrORwNL7YGKSiQ)

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
  - Consistency: All contract events are processed without exception. Should a non-deterministic error occur (e.g., temporary network or hardware issues), the system must fail and cause the node to crash. Upon node restart, the event that encountered an error will be reprocessed, ensuring no event is missed in the syncing process. Eth1 syncing would not continue past this event until it has been successfully processed.
- Re-org handling, be able to provide up-top-date data even when re-orgs happen

## :wrench: Design Components

- `Eth1Client` - A wrapper around Geth api client, used to communicate with the API only.
- `EventSyncer` - Reads events from `Eth1Client`, arranges them by the desired structure and shoot to subscribers (maybe only one?)
- `EventDataHandler` - Handles the data aspects of the events, process each block as a DB transaction. has access to database. can shoot the event data or process data again after finishing successfull tx. this component also updates what we know today as "SyncOffset", can be named "LastProcessedBlock".
- `operator/node` - Glues together options to create the `Eth1Client`, `EventSyncer` and `EventDataHandler`.
- `operator/validator` use of successfully handeled data events (`EventDataHandler`) to execute tasks (validator start/stop, etc)

## :question: APIs, suggestions and examples

### Resources

- [Flow chart](https://pasteboard.co/rmaBLhc78cfB.png)
- [Contract event list](https://docs.google.com/spreadsheets/d/1I8buKkZxYgrlSlL4MbXUGULvUsNZj50MQMDji1cnA7o/edit#gid=0)

### event block batches db tx psuedo-code

```
- Got event
- Batch events by block
- Got new block, save to next batch

    - Begin tx
        for event in batch
            - Event-specific read/writes, such as...
                - Update account nonce
                - Save validator share
            - Update sync offset to new block
    - Commit tx

```

suggestion - consider while syncing history batch all the events together and while ongiong batch only by block?

### Reorg handling

Currently we have decided to only read events up to finalized checkpoint blocks. We might need to ask eth2 node for this block number/hash but this can be done at `operator/node` and passed into the `EventBatcher` for separation.

### might be interesting -

- [log-flume](https://github.com/openrelayxyz/log-flume/blob/master/flumeserver/datafeed/websockets.go#L49)
- safeethclient
- [logs scanner with gap missing](https://github.com/hydroscan/hydroscan-api/blob/master/task/event_subscriber.go)

### Contract ABI

Use contract Binding for parsing event, generated code will mean no need to update on ABI change. (only generate).
ex: https://geth.ethereum.org/docs/developers/dapp-developer/native-bindings

### ETH1 API

- Seperate event DB handling and memory actions.

```golang
//option 1

client := eth1client.New(grpcURL, contractADDR, contractABI, contractBlock)


type BlockEvents struct {
    BlockNumber bigint
    Events []Event
}

// option 1
func EventsBatcher() <-chan BlockEvents{} {
batches := make(chan BlockEvents{})
    go func() {
        blockNumber := begin

        batch := BlockEvents{blockNumber}

        for log,err := client.StreamLogs(begin); err == nil         {
            if log.BlockNumber > blockNumber {
                batches <- batch
                blockNumber = log.BlockNumber
                batch = BlockEvents{log.BlockNumber}
            }

            batch.Events = append(batch.Events, log)
        }
    }
    return batches
}

// option2
func EventsBatcher() <-chan BlockEvents{} {
batches := make(chan BlockEvents{})
    go func() {
        blockNumber := begin

        batch := BlockEvents{blockNumber}

        for block,err := client.WaitForFinalizedBlockAfter(begin); err == nil         {
            if block.BlockNumber > blockNumber {
                // extract relevant events from block
                // add events to batch
                batches <- batch
            }

        }
    }
    return batches
}
```

### EventDataHandler needs DB Access

```golang

for {

select {
    case batch := <-batches:
        StartDBTX()
        tasks := handleBatch() // tasks are added to list of operations that should be done after db change
        UpdateSyncOffset()
        DoneDBTX()
        tasksChannel <- tasks// shot meaningfull event aftermath
    case ctx.Done():
        return
}



```

### BadgerDB Tx example

```golang
txn := badger.NewTransaction(true)

for _, e := range entries {
    if err := txn.Set(e.Key, e.Value); err != nil {
    // XXX DB Error!, Rewind, retry, or quit
    }
}
if err := txn.Commit(); err != nil {
    // XXX DB Error!, Rewind, retry, or quit
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

### TBD integratio with ETH2

---

## Implementation details

### Eth1Client

Interacts with the underlying eth1 client and returns data prepared in a convenient format.

#### Has methods:

##### Exported:

- `FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs []ethtypes.Log, lastBlock uint64, err error)`

Fetches past blocks.
Instead of FilterLogs it should call a wrapper that calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events.

- `StreamLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log`

Streams logs to the returned channel.
Subscribes to block notifications and then requests logs with FilterLogs from `fromBlock` (or `previous_processed_block+1`) to `block_from_notification`.
Supports offset for `block_from_notification` (8 by default).
Might be useful: https://github.com/hydroscan/hydroscan-api/blob/master/task/event_subscriber.go.
Instead of FilterLogs it should call a wrapper that calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events.
If streaming fails, it should reconnect to the node and continue streaming.

- `IsReady(ctx context.Context) (bool, error)`

Returns if node eth1 is ready (not syncing)

##### Unexported:

- `connect(ctx context.Context) error`

Connects to the node.

- `reconnect(ctx context.Context)`

Reconnects to the node using exponential backoff.

#### Metrics:

- Malformed events
- Processed events
- Last seen block

#### Tests:

TBD

### NodeProber

Checks eth clients readiness.

##### Implemented in https://github.com/bloxapp/ssv/pull/948

### EventSyncer

Dispatches event handling. Gets data from Eth1Client, passes the data to the eventBatcher method, which then passes data to subscribers.

Note: Subscriber pattern may be redundant, consider calling EventDataHandler's method(s) from eventBatcher.

#### Has methods:

- `Start(syncOffset uint64) error`

Fetches past logs and passes them to subscribers for processing. Then it indicates that it's ready. Then runs a loop which retrieves data from Eth1Client event stream (StreamLogs) starring from syncOffset and passes it to the `eventBatcher` method. Before retrieval it should ask NodeProber if nodes are ready.

- `WaitUntilReady()`

Waits until past logs and tasks are processed. Blocks until all events and tasks are processed. For waiting for tasks to finish, instead of communicating between modules, consider using an orchestrator because the process should be synchronous. Consider checking eth nodes readiness here

- `Close() error`

Close the loop and event processing

- `AddSubscriber(subscriber EventSubscriber)`

Adds a subscriber for batched events. Consider replacing it with another pattern as we'll have only 1 subscriber

- `ongoingEventBatcher(events <-chan Event)`

Batches ongoing events from channel and streams batches to subscribers (HandleOngoingBlockEvents) as `<-chan BlockEvents` (BlockEvents represents a batch of events for one block).
It should also dispatch tasks (pass to TaskExecutor) after DB changes are done by HandleOngoingBlockEvents.

- `pastEventBatcher(events []Event)`

Batches past events from channel in `[]BlockEvents` and passes the batches to subscribers (HandlePastBlockEvents)

#### Metrics:

- Is ready
- Subscriber count

#### Tests:

TBD

### EventDataHandler

Gets batched events from EventSyncer (EventBatcher). Updates SyncOffset (LastProcessedBlock). Notifies about tasks from events.

#### Has methods:

- `HandleEventLog(blockEvents <-chan BlockEvents)`

Goes over the channel and calls processEvent for each BlockEvents synchronously.

- `processEvent(blockEvents BlockEvents) error`

Processes event batch as a DB transaction doing event-specific actions and updating SyncOffset in the DB (EventDB.SetSyncOffset).

- processing methods for each event type

List of all event types: https://docs.google.com/spreadsheets/d/1I8buKkZxYgrlSlL4MbXUGULvUsNZj50MQMDji1cnA7o/edit#gid=0

- `AddSubscriber(subscriber TaskSubscriber)`

Adds a subscriber for tasks. Consider replacing it with another pattern as we'll have only 1 subscriber

#### Metrics:

- Last processed block
- Event processing time
- Subscriber count

#### Tests:

TBD

### EventDB

Wraps BadgerDB access.

#### Has methods:

- `BeginTX()`

Begins DB transaction.

- `EndTX()`

Ends DB transaction, commiting the changes.

- `GetSyncOffset(uint64, error)`

Gets sync offset from the DB.

- `SetSyncOffset(blockNumber uint64) error`

Sets sync offset in the DB.

#### Tests:

TBD

### TaskExecutor

Handles event tasks. Is a struct in validator package. Needs validatorsMap, keyManager, recipientsStorage from validator controller.

#### Has methods:

- `HandleTasks(<-chan EventTask)`

Goes over event tasks. Has a type switch on event task type. Calls further processing methods depending on task type.

- methods for handling tasks:
  - `addValidator`
  - `removeValidator`
  - `liquidateCluster`
  - `reactivateCluster`
  - `updateFeeRecipientAddress`

Do the tasks accordingly.

#### Tests:

TBD

### operator

Creates EventSyncer, EventDataHandler, TaskExecutor. Starts EventSyncer asynchronously passing EventDB.GetSyncOffset to it. Subscribes EventDataHandler to EventSyncer. Subscribes TaskExecutor to EventDataHandler.
