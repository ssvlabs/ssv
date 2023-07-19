# Attester message validation

## Message reactions

Node can react to a message in three ways:
- `Accept` - message is valid, accepted and processed further.
- `Ignore` - message is invalid, ignored and not processed further, violation count is increased.
- `Reject` - message is rejected, ignored and not processed further, violation count is set to violation threshold, peer is banned for the current round.

## Semantic assertions

- Message validator maintains a state for each validator.
- Generally, if violation happens by a small margin, the message is ignored. If an assertion has a rule for ignoring, the rule has higher priority than this one.
- Generally, If violation happens by a large margin, the message is rejected. If an assertion has a rule for rejecting, the rule has higher priority than this one.
- Validator attests only once per epoch.
  - TODO: Need to check if upon reorg there can be more than one duty per epoch.
  - First violation is ignored. Further ones are rejected.
- Validator submits messages for slot N within slots [N, N+32).
  - According to spec, attestation must be submitted within an epoch (32 slots).
  - Consequence: Validator submits messages for slot N in within [0, 384) seconds after slot start.
  - Slots [N+32; N+42) are ignored. Further ones are rejected.
- Message round is in range [1, 12].
  - Given quick round duration is 2 seconds, slow round duration is 120 seconds, last quick round is 8. 8 quick rounds take 16 seconds. As submission must be no later than 384 seconds, there are 368 seconds left for slow rounds. 368 / 120 = 3.0666, so there are 4 slow rounds if rounded up. Therefore, maximal possible round is 8 + 4 = 12.
  - Violation is rejected.
- Message round is equal to estimated current round.
  - Violation by 1-3 rounds is ignored. Violation by more than 3 rounds is rejected.
- If message slot is greater than current slot, message epoch is greater than current epoch.
  - Violation is rejected.
- Stage assertions:
  - If current stage is `proposal`, next messages cannot be `proposal`.
  - If current stage is `prepare`, next messages cannot be `prepare`.
  - If current stage is `commit`, next messages cannot be `proposal`, nor `prepare`, nor `commit`.
  - If current stage is `quorum`, next messages cannot be `proposal`, nor `prepare`, nor `commit`. Each message increases quorum count which must be less than or equal to 3f+1.
  - If current stage is `postConsensus`, no further messages can be submitted.
  - First violation in round is ignored. Further ones are rejected.
- If message slot is equal to current slot, validator submits up to 3f+5 messages for each slot-round pair:
  - 1 proposal
  - 1 prepare
  - 1 commit
  - 3f+1 aggregated commit/decided
  - 1 post-consensus
  - Violation by [1, f] message is ignored. Violation by more than that is rejected.
- If violation count reaches a certain threshold, all further messages from the validator for current round are rejected.
- Each round resets violation count to 0.

### Validation state

```go
type stage int

const (
    proposal stage = iota
    prepare
    commit
    quorum
    postConsensus
)

type SignerState struct {
    Slot          phase0.Slot
    Round         specqbft.Round
    Stage         stage
    QuorumCnt     int
    ViolationCnt  int
}
```

### Estimated round calculation 

```go
func calculateEstimatedRound() uint64 {
    firstRoundStart := slot.StartTime() + waitAfterSlotStart
    sinceFirstRound := message.Time() - firstRoundStart
    if currentQuickRound := 1 + sinceFirstRound / quickRoundDuration; currentQuickRound <= lastQuickRound {
        return currentQuickRound
    }

    sinceFirstSlowRound := message.Time() - (firstRoundStart + lastQuickRound * quickRoundDuration)
    return lastQuickRound + 1 + sinceFirstSlowRound / slowRoundDuration
}
```




