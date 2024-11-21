# Builder proposals

## How to use

1. Configure your beacon node to use an external builder
   - Lighthouse: https://lighthouse-book.sigmaprime.io/builders.html
   - Prysm: https://docs.prylabs.network/docs/prysm-usage/parameters

## How it works

### Blinded beacon block proposals 

If builder proposals are enabled, 
the SSV node attempts to get/submit blinded beacon block proposals (`/eth/v1/beacon/blinded_blocks`) to beacon node
instead of regular ones (`/eth/v1/beacon/blocks`). 

### Validator registrations

If builder proposals are enabled, the SSV node regularly submits validator registrations according to the following logic:

- Registration for each validator is submitted to registrations collector every 10 epochs. To reduce beacon node load, slot for submission is chosen according to the validator index.
- The first registration after the SSV node start is an exception to the rule above to avoid waiting up to 10 epochs: All validator registrations are submitted within 32 slots after the node start according to the validator index.
- Registration collector submits queued validator registrations to beacon node once per epoch. The slot index within an epoch is different for each operator and is calculated based on operator ID to reduce beacon node load. The maximal amount of registrations in one request is 500. If the queue contains more than that, all queued registrations are submitted by chunks of 500 registrations without a delay. 

## Known issues

- Builder proposals don't work with Prysm as it returns `400 Unsupported block type` when requesting a blinded block.

## Edge cases outcomes

### Scenario 1. 4 operators, 4 BNs, 4 MEVs, MEV1&2 use relay-1, MEV3&4 use relay-2

- Blinded block header received from MEV, local block is more profitable

Local block is successfully submitted and shown on beaconchain as regular, non-MEV block

- Blinded block header received from MEV more than once in the same slot

Successful receipt, no error, block hashes may be same, may be different

- Nodes receive same MEV block hashes, round leader proposes its received block hash for consensus, any node submits it

The first submitter using the same relay, whether or not it's the round reader, successfully submits the block, it's shown as MEV on beaconchain. Others fail to submit the block due to "no successful relay response"

- Nodes receive different MEV block hashes, round leader proposes its received block hash for consensus, any node submits it

The first submitter using the same relay, whether or not it's the round reader, successfully submits the block, it's shown as MEV on beaconchain. Others fail to submit the block due to "no successful relay response"


### Scenario 2. 4 operators, 4 BNs, 4 MEVs using 3 shared relays

- Blinded block header received from MEV, local block is more profitable	

Local block is successfully submitted and shown on beaconchain as regular, non-MEV block

- Blinded block header received from MEV more than once in the same slot

Successful receipt, no error, block hashes may be same, may be different

- Nodes receive same MEV block hashes, round leader proposes its received block hash for consensus, any node submits it

The first submitter, whether or not it's the round reader, successfully submits the block, it's shown as MEV on beaconchain, others fail to submit the block due to "no successful relay response"

- Nodes receive different MEV block hashes, round leader proposes its received block hash for consensus, any node submits it

The first submitter, whether or not it's the round reader, successfully submits the block, it's shown as MEV on beaconchain, others fail to submit the block due to "no successful relay response"

### Scenario 3. 4 operators, 2 have MEV on, 2 have MEV off

- Round leader has MEV on

Nodes having MEV off fail to validate the proposed MEV block as input data and return "blinded blocks are not supported", so the consensus in the round is not met. Nodes proceed to the next round and choose the next round leader

- Round leader has MEV off
 
Nodes run consensus on a regular non-MEV block and submit it
