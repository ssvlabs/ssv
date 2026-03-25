---
title: Update Proposal Validation for SMR
base-branch: main
---

## Task: Update Proposal Validation for (5f-1)-SMR

### Background
In (5f-1)-SMR, proposals for the first block of a view require different validation
than subsequent blocks. The proposal must include proof (TC or status messages)
justifying the proposed block.

### Spec Reference (Section 4.1 and 4.2)

**Proposal Format**: `⟨propose, ⟨B_k, w⟩_{L_w}, C, S⟩_{L_w}` where:
- B_k: the new block
- C: a valid QC for the parent of B_k
- S: proof for first block of view (TC or status messages), empty otherwise

**Vote Validation Condition**:
- If first block of view: Verify S where S must be either:
  - (a) a valid TC of view w-1 locking B_k, OR
  - (b) a set of 4f-1 valid status messages where B_k is locked by highest TC in S
  Also verify C is the QC for B_k's parent
- If subsequent block: Verify B_k extends the highest certified block known

### Requirements

1. **Create SMR proposal types** in `protocol/v2/qbft/smr/proposal.go`:
   ```go
   type SMRProposal struct {
       Block       *Block
       View        uint64
       ParentQC    *QuorumCertificate
       Proof       SMRProof  // TC or StatusSet
       IsFirstBlock bool
   }
   
   type SMRProof interface {
       Type() ProofType  // ProofTypeTC or ProofTypeStatusSet
       Validate(committee *CommitteeMember) error
       LocksBlock(block *Block, leaderID OperatorID) bool
   }
   
   type TCProof struct {
       TC *TimeoutCertificate
   }
   
   type StatusSetProof struct {
       StatusMessages []*StatusMessage
   }
   ```

2. **Implement proposal validation** in `protocol/v2/qbft/smr/proposal_validation.go`:
   ```go
   func ValidateSMRProposal(
       proposal *SMRProposal,
       committeeMember *CommitteeMember,
       knownHighestCertified *Block,
       previousLeader OperatorID,
   ) error
   ```
   
   Validation steps:
   - Verify proposer is leader for the view
   - Verify block integrity (hash, parent link)
   - If first block: validate proof (TC or status set) locks the block
   - If subsequent block: verify extends highest certified
   - Verify ParentQC is valid for block's parent

3. **Implement TCProof validation**:
   - Verify TC is valid (4f-1 signatures, proper view)
   - Verify TC locks the proposed block using locking conditions

4. **Implement StatusSetProof validation**:
   - Verify 4f-1 valid status messages
   - Find highest TC within status messages
   - Verify proposed block matches what highest TC locks

5. **Integrate with existing isValidProposal**:
   - In `protocol/v2/qbft/instance/proposal.go`, modify `isValidProposal`
   - When IsSMRMode() and first round, use SMR validation
   - Keep existing validation for QBFT mode and non-first-round SMR

6. **Add tests** in `proposal_validation_test.go`:
   - First block with valid TC proof
   - First block with valid status set proof
   - First block with invalid/missing proof
   - Subsequent block validation
   - Block not locked by proof
   - Invalid QC for parent

### Files to Create/Modify
- `protocol/v2/qbft/smr/proposal.go` (new)
- `protocol/v2/qbft/smr/proposal_validation.go` (new)
- `protocol/v2/qbft/smr/proposal_validation_test.go` (new)
- `protocol/v2/qbft/instance/proposal.go` (modify)

### Dependencies
- Requires smr/timeout-certificate
- Requires smr/status-messages
- Requires smr/quorum
