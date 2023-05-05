# MEV edge cases

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
