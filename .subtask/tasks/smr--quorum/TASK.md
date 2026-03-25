---
title: Update Quorum Calculation for (5f-1)-SMR
base-branch: main
---

## Task: Update Quorum Calculation for (5f-1)-SMR Protocol

### Background
The current SSV implementation uses standard QBFT quorum calculation (2f+1 with n >= 3f+1).
The (5f-1)-SMR protocol requires different quorum sizes: 4f-1 with n >= 5f-1.

### Spec Reference
- Resilience: n >= 5f-1 replicas, tolerating f Byzantine faults
- Quorum Certificate (QC): 4f-1 distinct signed vote messages
- Timeout Certificate (TC): 4f-1 signed timeout messages

### Requirements

1. **Add SMR-specific quorum functions** in `protocol/v2/types/ssvshare.go`:
   - `ComputeSMRF(committeeSize uint64) uint64` - compute f where n >= 5f-1, so f = (n+1)/5
   - `ComputeSMRQuorum(committeeSize uint64) uint64` - returns 4f-1
   - `ValidSMRCommitteeSize(committeeSize uint64) bool` - validates n >= 5f-1

2. **Add SMR quorum methods to CommitteeMember** or create wrapper:
   - `HasSMRQuorum(cnt int) bool` - checks if cnt >= 4f-1
   - `GetSMRQuorum() uint64` - returns 4f-1

3. **Keep existing QBFT functions** unchanged for backward compatibility

4. **Add unit tests** for:
   - SMR quorum calculation for various committee sizes (4, 7, 10, 13)
   - Edge cases and boundary conditions
   - Comparison between QBFT and SMR quorums

### Files to Modify
- `protocol/v2/types/ssvshare.go` - add SMR quorum functions
- `protocol/v2/types/ssvshare_test.go` - add tests

### Example Calculations
| n (committee) | f (QBFT) | QBFT Quorum | f (SMR) | SMR Quorum |
|---------------|----------|-------------|---------|------------|
| 4             | 1        | 3           | 1       | 3          |
| 7             | 2        | 5           | 1       | 3          |
| 10            | 3        | 7           | 2       | 7          |
| 13            | 4        | 9           | 2       | 7          |

Note: SMR allows smaller f for same n, but requires larger quorum relative to f.
