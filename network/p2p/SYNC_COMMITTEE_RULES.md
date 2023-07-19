# Sync committee message validation

## Semantic assertions

- Validator submits messages for slot N within slots [N, N+32).
  - According to spec, attestation must be submitted within an epoch (32 slots).
  - Consequence: Validator submits messages for slot N in within [0, 384) seconds after slot start.
  - Slots [N+32; N+42) are ignored. Further ones are rejected.
- Message round is in range [1, 12].
  - Given quick round duration is 2 seconds, slow round duration is 120 seconds, last quick round is 8. 8 quick rounds take 16 seconds. As submission must be no later than 384 seconds, there are 368 seconds left for slow rounds. 368 / 120 = 3.0666, so there are 4 slow rounds if rounded up. Therefore, maximal possible round is 8 + 4 = 12.
  - Violation is rejected.
- If message slot is equal to current slot, validator submits up to f+5 messages for each slot-round pair:
  - 1 proposal
  - 1 prepare
  - 1 commit
  - f+1 aggregated commit/decided
    - f+1 is all possible quorum sizes
  - 1 post-consensus
  - Violation by [1, f] message is ignored. Violation by more than that is rejected.





