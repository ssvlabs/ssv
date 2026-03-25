---
title: Implement View Change Handler using existing TC types
base-branch: smr/full-implementation
---

## Task: Implement View Change Handler for SMR

### Context
We have a foundation in `protocol/v2/qbft/smr/` with:
- `timeout_certificate.go` - TimeoutCertificate with Messages array, TimeoutMessage, and locking logic
- `block.go` - Block type with View, Height, Root fields

You MUST use these existing types. Do NOT redefine them.

### Requirements

1. **Create `view_change.go`** in `protocol/v2/qbft/smr/`:

   ```go
   type TimeoutManager struct {
       View              uint64
       HighestVotedBlock *Block  // use existing Block type
       TimeoutMessages   map[OperatorID]*TimeoutMessage // use existing type
       HighestTC         *TimeoutCertificate
   }
   ```
   
   Methods:
   - `GenerateTimeout(signer OperatorSigner) (*TimeoutMessage, error)`
   - `AddTimeoutMessage(msg *TimeoutMessage) error`
   - `CanFormTC(cm *CommitteeMember, prevLeader OperatorID) bool` - per spec 5.2
   - `FormTC() *TimeoutCertificate`

2. **Create ViewChangeHandler**:
   ```go
   type ViewChangeHandler struct {
       CurrentView     uint64
       TimeoutManager  *TimeoutManager
       CommitteeMember *CommitteeMember
   }
   ```
   
   Methods:
   - `OnTimeout() (*TimeoutMessage, error)` - generate and return timeout msg
   - `OnTimeoutMessage(msg *TimeoutMessage) error` - process incoming
   - `TryEnterNewView() (newView uint64, tc *TimeoutCertificate, ok bool)`

3. **Spec compliance (Section 5.1-5.2)**:
   - Timeout trigger: generate timeout message with highest voted block
   - TC formation: collect 4f-1 timeout messages where either:
     - (a) no conflicting blocks from previous leader, OR
     - (b) messages not from previous leader
   - Use existing TimeoutCertificate methods for validation

4. **Tests**: Create `view_change_test.go` covering:
   - Timeout generation
   - TC formation with various scenarios
   - View entry conditions

### Files
- Create: `protocol/v2/qbft/smr/view_change.go`
- Create: `protocol/v2/qbft/smr/view_change_test.go`
- Do NOT modify: `timeout_certificate.go`, `block.go`
