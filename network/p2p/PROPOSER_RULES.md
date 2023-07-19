# Proposer message validation

## Semantic assertions

- Validator submits messages for slot N only in slot N.
  - According to spec, each slot has a limited time window to propose their block, typically the duration of the slot itself (12 seconds).
  - Consequence: Validator submits messages for slot N in within [0, 12] seconds after slot start.
  - Slots [N+1; N+32) are ignored. Further ones are rejected.
- Message round is in range [1, 6].
  - Given quick round duration is 2 seconds. As submission must be no later than 12 seconds, 12 / 2 = 6. So, maximal possible round is 6.
  - Rounds [7; 12] are ignored. Further ones are rejected.
- If message slot is equal to current slot, validator submits up to f+6 messages for each slot-round pair:
  - 1 pre-consensus
  - 1 proposal
  - 1 prepare
  - 1 commit
  - f+1 aggregated commit/decided
    - f+1 is all possible quorum sizes
  - 1 post-consensus
  - Violation by [1, f] message is ignored. Violation by more than that is rejected.





