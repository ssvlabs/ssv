# Attester message validation

## Message reactions

Node can react to a message in three ways:
- `Accept` - message is valid, accepted and processed further.
- `Ignore` - message is invalid, ignored and not processed further, violation is noted.
- `Reject` - message is rejected, ignored and not processed further, peer is banned.

## Attester assertions

- Message validator maintains a state for each validator.
- Validator attests only once per epoch.
- Validator submits messages for slot N within slots [N, N+32).
- Validator submits messages for slot N in within [0, 384) seconds after slot start.
- Message round is in range [1, 12].
- Message round is equal to estimated current round.
- If message slot is greater than current slot, message epoch is greater than current epoch.
- If current stage is `proposal`, next messages cannot be `proposal'`.
- If current stage is `prepare`, next messages cannot be `prepare`.
- If current stage is `commit`, next messages cannot be `proposal`, nor `prepare`, nor `commit`.
- If current stage is `quorum`, next messages cannot be `proposal`, nor `prepare`, nor `commit`. Each message increases quorum count which must be less than or equal to 3f+1.
- If current stage is `postConsensus`, no further messages can be submitted.
- If message slot is equal to current slot, validator submits up to 3f+5 messages for each slot-round pair:
  - 1 proposal
  - 1 prepare
  - 1 commit
  - 3f+1 aggregated commit/decided
  - 1 post-consensus
- If violation happens by a small margin, the message is ignored and the violation count is increased.
- If violation happens by a large margin, the message is rejected and the violation count is set to violation threshold.
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
    sinceFirstRound := message.Time() - firstRoundStart`
    if currentQuickRound := 1 + sinceFirstRound / quickRoundDuration; currentQuickRound <= lastQuickRound {
        return currentQuickRound
    }

    sinceFirstSlowRound := message.Time() - (firstRoundStart + lastQuickRound * quickRoundDuration)
    return lastQuickRound + 1 + sinceFirstSlowRound / slowRoundDuration
}
```




