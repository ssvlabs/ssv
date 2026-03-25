---
title: Implement SMR Proposal Validation using existing types
base-branch: smr/full-implementation
---

## Task: Implement SMR Proposal Validation

### Context
We have a foundation in `protocol/v2/qbft/smr/` with:
- `timeout_certificate.go` - TimeoutCertificate with LocksBlock() method
- `block.go` - Block type with View, Height, Root fields

You MUST use these existing types. Do NOT redefine them.

### Requirements

1. **Create `proposal.go`** in `protocol/v2/qbft/smr/`:

   ```go
   type SMRProposal struct {
       Block        *Block  // use existing type
       View         uint64
       ParentQC     *QuorumCertificate
       Proof        SMRProof
       IsFirstBlock bool
   }
   
   type SMRProof interface {
       Type() ProofType
       Validate(cm *CommitteeMember) error
       LocksBlock(block *Block, leaderID OperatorID) bool
   }
   
   type TCProof struct {
       TC *TimeoutCertificate  // use existing type
   }
   
   type StatusSetProof struct {
       StatusMessages []*StatusMessage  // will be added by smr/status-v2
   }
   ```

2. **Create `proposal_validation.go`**:
   ```go
   func ValidateSMRProposal(
       proposal *SMRProposal,
       cm *CommitteeMember,
       knownHighestCertified *Block,
       previousLeader OperatorID,
   ) error
   ```
   
   Validation per spec 4.2:
   - First block of view: verify proof locks the proposed block
     - TCProof: use TC.LocksBlock(block, leader)
     - StatusSetProof: find highest TC, check it locks block
   - Subsequent block: verify extends highest certified

3. **Integration with existing QBFT**:
   - Modify `protocol/v2/qbft/instance/proposal.go`
   - In `isValidProposal`, when `IsSMRMode()` and first round, use SMR validation
   - Keep existing validation for non-SMR mode

4. **Tests**: Create `proposal_test.go` covering:
   - First block with valid TC proof
   - First block with valid status set proof
   - First block with invalid proof
   - Subsequent block validation

### Files
- Create: `protocol/v2/qbft/smr/proposal.go`
- Create: `protocol/v2/qbft/smr/proposal_validation.go`
- Create: `protocol/v2/qbft/smr/proposal_test.go`
- Modify: `protocol/v2/qbft/instance/proposal.go` (add SMR integration)
