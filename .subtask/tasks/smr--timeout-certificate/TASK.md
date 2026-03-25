---
title: Implement Timeout Certificate (TC) Data Structure
base-branch: main
---

## Task: Implement Timeout Certificate (TC) Data Structure

### Background
The (5f-1)-SMR protocol uses Timeout Certificates (TC) for view changes.
A TC proves that enough replicas want to change views and "locks" a block.

### Spec Reference (Section 2.2 and 6.1)

**Timeout Certificate (TC)**: A certificate T_w locks a block B for view w.
It requires a quorum of 4f-1 signed timeout messages.

**TC Locking Logic** - T_w locks B if it contains >= 4f-1 timeout messages AND:

**Condition 1:**
- T_w contains >= 2f-1 messages for blocks B' such that B extends or equals B'
- T_w contains no messages for blocks that conflict with B

**Condition 2:**
- T_w contains >= 2f messages for blocks B' such that B extends or equals B'
- T_w contains no timeout message from the leader L_w

If multiple blocks satisfy these conditions, T_w locks the highest one.

**Conflicting Blocks**: Two blocks are conflicting if neither extends the other.

### Requirements

1. **Create TC data structures** in new file `protocol/v2/qbft/smr/timeout_certificate.go`:
   ```go
   type TimeoutMessage struct {
       View       uint64
       Block      *Block  // highest block voted for, or nil
       SignerID   OperatorID
       Signature  []byte
   }
   
   type TimeoutCertificate struct {
       View     uint64
       Messages []*TimeoutMessage
   }
   ```

2. **Implement TC methods**:
   - `NewTimeoutCertificate(view uint64) *TimeoutCertificate`
   - `(tc *TC) AddMessage(msg *TimeoutMessage) error` - validate and add
   - `(tc *TC) HasQuorum(committeeMember *CommitteeMember) bool` - check 4f-1
   - `(tc *TC) IsValid(committeeMember *CommitteeMember) bool` - full validation
   - `(tc *TC) LocksBlock(block *Block, leaderID OperatorID) bool` - check locking conditions
   - `(tc *TC) GetLockedBlock(leaderID OperatorID) *Block` - get highest locked block

3. **Implement helper functions**:
   - `BlockExtends(child, parent *Block) bool` - check if child extends parent
   - `BlocksConflict(a, b *Block) bool` - check if blocks are on different forks
   - `HighestBlock(blocks []*Block) *Block` - get highest by view then height

4. **Add validation**:
   - Verify signatures on timeout messages
   - Check signer is in committee
   - Check view number consistency
   - Prevent duplicate signers

5. **Add comprehensive tests** in `timeout_certificate_test.go`:
   - TC construction and validation
   - Locking condition 1 scenarios
   - Locking condition 2 scenarios
   - Conflicting block detection
   - Edge cases (empty TC, single message, etc.)

### Files to Create/Modify
- `protocol/v2/qbft/smr/timeout_certificate.go` (new)
- `protocol/v2/qbft/smr/timeout_certificate_test.go` (new)
- `protocol/v2/qbft/smr/block.go` (new, if needed for Block type)
