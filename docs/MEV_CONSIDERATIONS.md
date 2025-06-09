## Getting started with `MEV` configuration

To get the most out of MEV opportunities Operator can configure ProposerDelay configuration setting using a configuration
file (or `PROPOSER_DELAY` environment variable):
```
ProposerDelay: 300ms
```

As per our own estimates the max reasonable value of `ProposerDelay` for Ethereum mainnet is around ~1.65s, 
although we recommend starting with something like 300ms gradually increasing it up - the higher 
`ProposerDelay` value is the higher the chance of missing Ethereum block proposal will be.

### Important Safety Limitation

**The SSV node will refuse to start if ProposerDelay is set higher than 1.65s without explicit confirmation.**

If you attempt to use a ProposerDelay value higher than 1.65s, the node will exit with an error message. 
If you understand the risks and want to proceed anyway, you must also set the `AllowDangerousProposerDelay` flag:

```yaml
ProposerDelay: 2000ms
AllowDangerousProposerDelay: true
```

Or using environment variables:
```bash
PROPOSER_DELAY=2000ms ALLOW_DANGEROUS_PROPOSER_DELAY=true ./bin/ssvnode start-node
```

**Warning:** Using ProposerDelay values higher than 1.65s significantly increases the risk of missing block proposals, 
which can result in penalties and lost rewards.

As per the notes in other sections of this document `ProposerDelay` depends on a number of things, to find
the best value Operator might want to start with lower values like 300ms gradually increasing it up.

## MEV considerations & SSV proposer-duty flow background

To understand how MEV fits with the SSV cluster, here is some background on the SSV proposer-duty flow:
- SSV node participates in the pre-consensus phase to build RANDAO signature that will be used when 
  requesting block from Beacon node (let's say it takes `RANDAOTime`)
- SSV node (all nodes in the cluster really to handle round-changes, but current round Leader 
  specifically) requests blinded block header from Beacon node which in turn "proxies" this request 
  to MEV-boost that runs with some pre-configured timeout (call it `MEVBoostRelayTimeout`)
- MEV-boost sends multiple requests to Relays it knows about and waits until that 
  `MEVBoostRelayTimeout` time to choose the best block (based on the corresponding bid)
- SSV node receives the response with the chosen block header and goes through QBFT consensus phase 
  to sign it as Validator (let's say it takes `QBFTTime` at most - we can estimate it 
  statistically with some probability/confidence)
- QBFT consensus phase might require several rounds to complete in case there is a fault with the
  chosen round leader, each round can take up to `RoundTimeout` (currently set to 2s on SSV-protocol 
  level, which also means there will be 2 rounds at most because Ethereum block must be proposed 
  within 4s from slot start) meaning if round 1 doesn't complete in under `RoundTimeout` another 
  leader will be chosen to try and complete QBFT in round 2, etc.
- once QBFT completes successfully, Operator needs to submit the signed block to Beacon node to 
  propagate it throughout Ethereum network (call it `BlockSubmissionTime`)
- there is some time spent on executing various code to "glue" this whole thing together 
  that's small but still matters (call it `MiscellaneousTime`)

and so this means for the best SSV cluster operations we want the following condition to always hold true:
```
RANDAOTime + MEVBoostRelayTimeout + QBFTTime + BlockSubmissionTime + MiscellaneousTime < 4s
```
if this equation doesn't hold, Validator will miss his opportunity to propose the block (slot will be 
missed if the corresponding Ethereum block isn't published within 4s after slot start time)

and so the most straightforward approach to extract highest MEV is (and it probably works out-of-the box 
with SSV nodes already, but with caveats mentioned below):

### approach 1

To set `MEVBoostRelayTimeout` as high as possible, and have plenty of Relays 
(MEV-boost is configured with) so that Relays themselves decide "how much time they want to wait 
since Slot start before replying with a block/bid" - some Relays would be fast to reply but not 
as profitable as those that withhold for longer

^ the problem of this approach is that SSV node Operator might not be able to find/choose Relays 
that fit his MEV desires (maybe he can only use those that respond right away without any additional 
delay for the sake of higher MEV)

thus an alternative approach would be:

### approach 2

To introduce an additional configurable delay `ProposerDelay` SSV Operator can set so 
that it will work nicely even with Relays that "reply as fast as possible", the equation from above
becomes:
```
RANDAOTime + ProposerDelay + MEVBoostRelayTimeout + QBFTTime + BlockSubmissionTime + MiscellaneousTime < 4s
```
plugging in some realistic numbers into that ^ formula we get a rough estimate of ~2.2s for `ProposerDelay`: 
```go
const randaoTime = 100 * time.Millisecond
const mevBoostRelayTimeout = 200 * time.Millisecond
const qbftTime = 350 * time.Millisecond
const miscellaneousTime = 150 * time.Millisecond
const blockSubmissionTime = 1000 * time.Millisecond
const proposerDelay = 4*time.Second - randaoTime - mevBoostRelayTimeout - qbftTime - blockSubmissionTime - miscellaneousTime
```
but on top of that another consideration Operator needs to take into account is - other SSV nodes in 
his cluster might not even have ProposerDelay configured (it's 0s by default), meaning they will start QBFT 
sooner and timeout round 1 sooner. To prevent that round timeout we'll need to cap proposerDelay accordingly 
so it does not exceed that `QBFTConstrainingTime` - this would give us a rough estimate of ~1.65s:
```go
const qbftConstrainingTime = roundtimer.QuickTimeout - qbftTime
```

Therefore, we consider ~1.65s to be the maximum reasonable value of `ProposerDelay` that an Operator should be able to use safely.

**To enforce this safety limit, the SSV node will automatically prevent startup if ProposerDelay exceeds 1.65s 
unless the operator explicitly acknowledges the risk by setting `AllowDangerousProposerDelay: true`.**
