---
title: Implement Status Messages using existing TC types
base-branch: smr/full-implementation
---

## Task: Implement Status Messages for SMR View Change

### Context
We have a foundation in `protocol/v2/qbft/smr/` with:
- `timeout_certificate.go` - TimeoutCertificate with Messages array and proper locking logic
- `block.go` - Block type with View, Height, Root fields

You MUST use these existing types. Do NOT redefine TimeoutCertificate, Block, or OperatorID.

### Requirements

1. **Create `status.go`** in `protocol/v2/qbft/smr/` with:
   ```go
   type StatusMessage struct {
       PreviousView uint64
       QC           *QuorumCertificate  // QC for parent of locked block
       HighestTC    *TimeoutCertificate // use existing type from timeout_certificate.go
       SignerID     OperatorID          // use existing alias
       Signature    []byte
   }
   ```

2. **Create StatusCollector** for leader:
   ```go
   type StatusCollector struct {
       View     uint64
       Messages map[OperatorID]*StatusMessage
   }
   ```
   
   Methods:
   - `HasQuorum(cm *CommitteeMember) bool` - check 4f-1 messages
   - `GetHighestTC() *TimeoutCertificate` - return highest TC from messages
   - `DetermineStartingBlock(leaderID OperatorID) *Block` - per spec 5.3

3. **Add QuorumCertificate type** (if not exists):
   ```go
   type QuorumCertificate struct {
       View      uint64
       BlockHash [32]byte
       Signers   []OperatorID
   }
   ```

4. **Ensure compatibility**:
   - Import and use types from the same package (no redefinition)
   - Use TimeoutCertificate.LocksBlock() and GetLockedBlock() methods
   - Tests should pass: `go test ./protocol/v2/qbft/smr/...`

### Spec Reference (Section 5.2-5.3)
- Status message: `⟨status, w-1, C, T_high⟩` sent to new leader
- Leader collects 4f-1 status messages
- Case A: If TC locks block B', propose B' with TC as proof
- Case B: Otherwise use block from highest TC in status set

### Files
- Create: `protocol/v2/qbft/smr/status.go`
- Create: `protocol/v2/qbft/smr/status_test.go`
- Do NOT modify: `timeout_certificate.go`, `block.go`
