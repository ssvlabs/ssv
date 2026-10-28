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
path stays the fallback whenever the overlay fails or a builder is unavailable. The design record is
[issue #2962](https://github.com/ssvlabs/ssv/issues/2962); coverage of the overlay on the local Gloas
testnet is tracked in [ssvlabs/aetheria#173](https://github.com/ssvlabs/aetheria/issues/173).

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
- `AuthData` defaults to the `URL`'s hostname — lowercased, with scheme, userinfo, port, path, query and
  fragment dropped, and an IPv6 literal in compressed hex-only form inside brackets (builder-specs'
  `get_default_auth_data`, the same rule other clients apply). So operators' `URL`s that differ only in a
  trailing slash, letter case, port or path still sign the same default. An internationalized hostname must
  be given in punycode, and a URL with an IPv6 zone needs an explicit `AuthData`. Entries whose URLs share a
  hostname share the default, and so one signed auth; builders that need distinct identities on one host
  agree an explicit `AuthData` instead.
- The `URL` itself isn't signed, so it may differ per operator as long as the `AuthData` it resolves to
  doesn't: when operators' hostnames differ, as when each points at its own sidecar, set `AuthData`
  explicitly (see [MEV-Boost and commit-boost after Gloas](#mev-boost-and-commit-boost-after-gloas)).
- The unsigned knobs (`MinBid`, `BuilderBoostFactor`, `MaxExecutionPayment`) don't affect signing, but
  divergence makes the cluster's effective bid policy depend on which operator leads the round — keep them
  identical too. They are sent on the `produceBlockV4` POST and honored by beacon nodes that implement it
  (beacon-APIs#630); against an older node the request falls back to GET, where only `BuilderBoostFactor`
  still applies (the GET's long-standing knob) — `MinBid` and the per-entry knobs are POST-only.
- Remote-signing operators (Web3Signer) cannot produce request-auth partials — there is no request-auth
  signing type there yet. A node with `Builders` entries set and a remote signer warns at startup and drops
  its direct-builder **entries** (keeping the top-level p2p knobs); the cluster still reconstructs auths
  while at most `f` operators are remote-signing.

### How it works

Once at least one builder is configured, three things happen around a proposal — all opt-in, all falling
back to the enshrined flow (gossiped bids / self-build) on any failure:

1. **Ahead-of-time auth.** Across the proposer lookahead, the §5 dispatcher threshold-signs one
   `BuilderRequestAuth{data, proposal_slot}` per builder and reconstructs it from operator partials into a
   per-slot cache.
2. **Bid request.** At proposal time the node sends `produceBlockV4` as a POST (beacon-APIs#630) carrying
   the config plus the reconstructed auths; the beacon node authenticates to each builder and returns the
   winning block. When a builder-API bid wins, the node echoes `Eth-Builder-Url` on publish so the beacon
   node forwards the block to that builder.
3. **Preferences.** On each reconstruction the node also submits the ahead-of-time
   `submitBuilderPreferences` (the `MaxExecutionPayment` cap) through its own beacon node, so the builder
   holds it before the bid request arrives.

A cluster with no `Builders` config produces over the same `produceBlockV4` POST, sending a neutral
local-build config (empty `builders`, `builder_boost_factor` 100). The 100 weighs gossiped p2p bids at par
with the local build — profit maximization, the same default beacon nodes applied to the pre-#630 GET — so
an unconfigured cluster behaves as before, which also means it follows p2p bids as soon as staked builders
gossip any. A cluster that prefers its own local build instead sets a knobs-only config —
`BuilderBoostFactor: 0` with no `Entries` — which the beacon node honors as "local unless unviable". A
beacon node that predates the #630 POST answers it with 404/405; the node falls back to the legacy GET for
that node, still carrying `BuilderBoostFactor` (the one knob that GET honors) — `MinBid` and the per-entry
knobs are POST-only.

### MEV-Boost and commit-boost after Gloas

A sidecar is no longer needed. The beacon node contacts each configured builder itself over the
`produceBlockV4` POST and picks the winning bid, which was MEV-Boost's job before the fork. There is no
blinded block to unblind, since the builder reveals the payload, and validator registrations give way to
proposer preferences, which SSV signs and gossips on its own. Pointing `Entries` straight at the relays is
the simplest setup.

An operator who keeps a sidecar anyway (for its bid-timing features, say) needs a version that serves the
Gloas builder-API endpoints, and configures it as follows:

- **One entry per relay, each with `URL` set to the sidecar.** The sidecar doesn't fan a bid request out to
  its relays: it forwards each request to the one relay whose auth data it carries.
- **`AuthData` set explicitly on every entry**, to the relay's identity: the 0x-hex of the relay's
  hostname, unless the relay agreed a different value. Omitted, it defaults to the sidecar's own hostname
  (`localhost`, `mev-boost`), which matches no relay, so the sidecar rejects every request and the entry
  silently gets no bids. The node warns at startup about any entry that omits `AuthData` while its URL host
  looks local: a loopback, private or link-local IP, `localhost`, a single-label name such as a Docker
  service name, or a `.localhost`, `.local` or `.internal` name.

```yaml
Builders:
  Entries:
    - URL: "http://mev-boost:18550"
      AuthData: "0x72656c61792d612e6578616d706c652e636f6d" # hex of "relay-a.example.com"
    - URL: "http://mev-boost:18550"
      AuthData: "0x72656c61792d622e6578616d706c652e636f6d" # hex of "relay-b.example.com"
```

To get the hex of a relay's hostname:

```bash
printf '0x%s\n' "$(printf '%s' relay-a.example.com | od -An -tx1 | tr -d ' \n')"
```

Clusters can mix the two setups. Only `AuthData` and the slot are signed, not the URL, so an operator
pointing at `https://relay-a.example.com` directly and another pointing at its own sidecar with `AuthData`
set to the hex of `relay-a.example.com` sign the same bytes, and their partial signatures combine. Either
way, each relay takes one of the at most 8 entries.

## Before Gloas: MEV-Boost/commit-boost and relays

> Everything below applies only until the Gloas fork. From the fork on, SSV proposes over `produceBlockV4`
> instead of blinded blocks, stops submitting validator registrations, and needs no MEV-Boost or
> commit-boost sidecar (see [MEV-Boost and commit-boost after Gloas](#mev-boost-and-commit-boost-after-gloas)).

### How to use

1. Configure your beacon node to use an external builder
   - Grandine: https://docs.grandine.io/builder_api_and_mev.html
   - Lighthouse: https://lighthouse-book.sigmaprime.io/advanced_builders.html
   - Lodestar: https://chainsafe.github.io/lodestar/run/beacon-management/mev-and-builder-integration/
   - Nimbus: https://nimbus.guide/external-block-builder.html
   - Prysm: https://prysm.offchainlabs.com/docs/configure-prysm/builder/
   - Teku: https://docs.teku.consensys.com/how-to/configure/builder-network
2. Beacon node will automatically provide blinded blocks to SSV node when it's possible

### How it works

#### Blinded beacon block proposals

The SSV node requests proposals with `produceBlockV3` (`/eth/v3/validator/blocks`), and the beacon node decides between a blinded block from its external builder and a full local block. The cluster always runs consensus on a blinded block: when the beacon node returns a full block, the SSV node swaps its execution payload for the payload header and keeps the full block for submission. A blinded block has the same root as its full block, so the operators' signature covers both.

After consensus, every operator submits the signed block to its beacon nodes. The leader of the deciding round submits its full block to `/eth/v2/beacon/blocks` if the decided block is the one its beacon node built. Every other operator submits the blinded block to `/eth/v2/beacon/blinded_blocks`, and its beacon node has to get the block's payload (unblind it) before it can publish it.

#### Validator registrations

The SSV node regularly submits validator registrations according to the following logic:

- Registration for each validator is submitted to registrations collector every 10 epochs. To reduce beacon node load, slot for submission is chosen according to the validator index.
- The first registration after the SSV node start is an exception to the rule above to avoid waiting up to 10 epochs: All validator registrations are submitted within 32 slots after the node start according to the validator index.
- Registration collector submits queued validator registrations to beacon node once per epoch. The slot index within an epoch is different for each operator and is calculated based on operator ID to reduce beacon node load. The maximal amount of registrations in one request is 500. If the queue contains more than that, all queued registrations are submitted by chunks of 500 registrations without a delay. 

### Clusters with mixed builder setups

The operators of a cluster don't need the same builder setup: some may run MEV-Boost or commit-boost and others not, and their sidecars may use different relays.

- Consensus isn't affected. The consensus value is always a blinded block, and its validation doesn't depend on where the block came from, so an operator whose beacon node has no builder accepts a builder block from the round leader.
- The cluster decides on the block a round leader fetched: a builder block or a local block, whichever that leader's beacon node returned.
- Some submissions are expected to fail, since a beacon node can publish a blinded block only if it can get the block's payload. A builder block fails on beacon nodes with no builder, or whose sidecar doesn't use the relay that served the bid. A local block can be published only by the operator whose beacon node built it. One successful submission is enough, so when the decided block came from another operator's beacon node, a failed submission is logged at debug level and the duty is reported as `not_published` in the `ssv.runner.duty.outcome` metric rather than `failed`. A failed submission of the operator's own block is still an error.
