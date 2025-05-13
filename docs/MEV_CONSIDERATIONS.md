## SSV proposer-duty flow background

In order to understand how MEV fits with the SSV cluster, here is some background on the SSV proposer-duty flow:
- SSV node (all nodes in the cluster really to handle round-changes, but current round Leader 
  specifically) requests blinded block header from Beacon node which in turn "proxies" this request 
  to MEV-boost that runs with some pre-configured timeout (call it `MEVBoostRelayTimeout`)
- MEV-boost sends multiple requests to Relays it knows about and waits until that 
  `MEVBoostRelayTimeout` time to choose the best block (based on the corresponding bid)
- SSV node receives the response with the chosen block header and goes through QBFT consensus phase 
  to sign it as Validator (let's say it takes `QBFTMaxExpectedTime` at most - we can estimate it 
  statistically with some probability/confidence)
- QBFT consensus phase might require several rounds to complete in case there is a fault with the
  chosen round leader, each round can take up to `RoundTimeout` (currently set to 2s on SSV-protocol 
  level, which also means there will be 2 rounds at most because Ethereum block must be proposed 
  within 4s since slot start) meaning if round 1 doesn't complete in under `RoundTimeout` another 
  leader will be chosen to try and complete QBFT in round 2, etc.
- there is some time spend on executing various code to "glue" this whole thing together 
  that's small but still matters (call it `MiscellaneousTime`), for example, signing RANDAO would be
  in this category

and so this means for the best SSV cluster operations we want the following condition to always hold true:
```
MEVBoostRelayTimeout + QBFTMaxExpectedTime + MiscellaneousTime < RoundTimeout
```
if it doesn't hold ^ round-change happens occasionally - which is fine if that's some SSV node fault 
(as in `QBFTMaxExpectedTime` is exceeded), but we wouldn't want round-change to happen due to 
`MEVBoostRelayTimeout` being set too high, for example, yet we want `MEVBoostRelayTimeout` to be as 
high as possible to be able to wait for as many Relays as possible to provide candidate-block bids 
to choose the best one

and so the most straightforward approach to extract highest MEV (and it probably works out-of-the box 
with SSV nodes already, but with caveats mentioned below) would be

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

To introduce an additional configurable delay `MEVDelay` SSV Operator can set so 
that it will work nicely even with Relays that "reply as fast as possible", there are downsides 
though:
- the condition from above that must hold true to avoid undesirable round-changes becomes a bit more 
  complex for Operator to properly configure his settings:
```
MEVDelay + MEVBoostRelayTimeout + QBFTMaxExpectedTime + MiscellaneousTime < RoundTimeout
```
- namely, Operator must know whether MEV-boost Relays he configured "reply as fast as possible" 
  (or "have delays of their own") because this will indirectly affect the decision for how large `MEVDelay` value should be

## How to choose `MEVDelay` value

As per the notes from above `MEVDelay` depends on a number of things, to find the best value Operator 
might want to start with lower values like 300ms gradually increasing it up (the higher `MEVDelay` 
value is the higher the chance of missing Ethereum block proposal will be).

It's probably best to keep `MEVDelay` under ~500ms to satisfy the equation from above (or lower 
`MEVBoostRelayTimeout` accordingly to push `MEVDelay` value higher):

```
MEVDelay + ~1000ms + ~350ms + ~150ms < 2000ms
```
