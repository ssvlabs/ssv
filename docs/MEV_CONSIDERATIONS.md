## SSV proposer-duty flow background

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
  propagate it throughout Etehreum network (call it `BlockSubmissionTime`)
- there is some time spend on executing various code to "glue" this whole thing together 
  that's small but still matters (call it `MiscellaneousTime`)

and so this means for the best SSV cluster operations we want the following condition to always hold true:
```
RANDAOTime + MEVBoostRelayTimeout + QBFTTime + BlockSubmissionTime + MiscellaneousTime < 4s
```
if this equation doesn't hold, Validator will miss his opportunity to propose block (slot will be missed)

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
that it will work nicely even with Relays that "reply as fast as possible", the equation from above
becomes:
```
RANDAOTime + MEVDelay + MEVBoostRelayTimeout + QBFTTime + BlockSubmissionTime + MiscellaneousTime < 4s
```

## How to choose `MEVDelay` value

As per the notes from above `MEVDelay` depends on a number of things, to find the best value Operator 
might want to start with lower values like 300ms gradually increasing it up (the higher `MEVDelay` 
value is the higher the chance of missing Ethereum block proposal will be).

To estimate the max reasonable value of `MEVDelay` we can put in the average numbers in the equation and
get something like ~1400ms:

```
~100ms + MEVDelay + ~1000ms + ~350ms + ~1000ms + ~150ms < ~4000ms
```
Note:
- these numbers from above are just rough estimates observed during our testing, use these "estimates" 
  at your own risk
- `MEVBoostRelayTimeout` value is set by Operator and can be adjusted in conjunction with `MEVDelay`,
  for example, 2s can be allocated between `MEVBoostRelayTimeout` and `MEVDelay` as 1s & 1s or 0.5s & 1.5s
  which would result in different MEV outcomes depending on the exact Relays Operator has configured his
  MEV-boost with
