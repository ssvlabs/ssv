# Builder proposals

> **ePBS / Gloas (EIP-7732).** The bulk of this page describes the pre-Gloas external-builder flow —
> out-of-protocol PBS via MEV-Boost/commit-boost and relays. At the Gloas fork, in-protocol (enshrined) PBS
> supersedes it: the proposer publishes a block committing to a builder's *bid* instead of fetching a
> blinded block from a relay, the builder reveals the execution payload separately, and a Payload
> Timeliness Committee attests to its on-time arrival. SSV runs these new duties automatically, with no
> operator configuration; the optional [direct-builder overlay](#epbs-direct-builder-overlay-gloas) below
> is the one Gloas surface that takes config. Gloas is not active on Ethereum mainnet yet (devnets only);
> this page will be revised as ePBS approaches mainnet.

## ePBS direct-builder overlay (Gloas)

On top of the enshrined flow — gossiped bids from staked builders, with local self-build as the
always-available floor — a cluster MAY additionally maintain **direct builder connections**: authenticated
bid requests and per-builder bid preferences, per the Gloas
[builder-specs](https://github.com/ethereum/builder-specs/blob/master/specs/gloas/validator.md) and
[beacon-APIs#630](https://github.com/ethereum/beacon-APIs/pull/630). This is an **opt-in enhancement, not
on the critical path**: a cluster that never configures it still proposes valid blocks, and the enshrined
path stays the fallback whenever the overlay fails or a builder is unavailable. Design and rollout are
tracked in [issue #2962](https://github.com/ssvlabs/ssv/issues/2962).

Configuration is the `Builders` block (see `config.example.yaml`), using the ecosystem's
[keymanager-APIs#88](https://github.com/ethereum/keymanager-APIs/pull/88) `BuilderConfig` vocabulary:
top-level `MinBid` and `BuilderBoostFactor` (applied to p2p bids, and the default for any entry that omits
its own) plus an `Entries` list — each entry `URL`, `AuthData`, optional `BuilderPubKeys`,
`MaxExecutionPayment`, `MinBid`, `BuilderBoostFactor`.

**Every operator of every committee sharing a validator MUST configure the identical list — all `n`
operators, not just a quorum.** The builder authenticates the cluster by one BLS signature over
`BuilderRequestAuth{data, slot}` reconstructed from operator partials, and the partials only combine over
byte-identical `data`:

- `AuthData` divergence on a builder entry splits the signing quorum and **silently disables that builder**
  for the affected proposal slots — proposals still succeed via gossiped bids or self-build, so watch the
  build-source metrics rather than proposal failures.
- `AuthData` defaults to the UTF-8 bytes of `URL` exactly as configured — so even trailing-slash or case
  differences between operators' `URL` values break the quorum unless an explicit shared `AuthData` is set.
- The unsigned knobs (`MinBid`, `BuilderBoostFactor`, `MaxExecutionPayment`) don't affect signing, but
  divergence makes the cluster's effective bid policy depend on which operator leads the round — keep them
  identical too. They take effect with the produceBlockV4 POST migration (beacon-APIs#630).
- Remote-signing operators (Web3Signer) cannot produce request-auth partials — there is no request-auth
  signing type there yet. A node with `Builders` entries set and a remote signer warns at startup and disables the
  overlay locally; the cluster still reconstructs auths while at most `f` operators are remote-signing.

## How to use

1. Configure your beacon node to use an external builder
   - Lighthouse: https://lighthouse-book.sigmaprime.io/builders.html
   - Prysm: https://docs.prylabs.network/docs/prysm-usage/parameters
2. Beacon node will automatically provide blinded blocks to SSV node when it's possible

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
