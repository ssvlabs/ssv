---
title: Consolidate SMR implementations from all branches
base-branch: main
---

## Task: Consolidate SMR Implementations

### Problem
The SMR implementation was split across multiple parallel subtasks that each ran in isolated workspaces. This resulted in **duplicate and incompatible type definitions**:

- `smr/timeout-certificate` branch: Complete TimeoutCertificate with proper locking logic
- `smr/proposal-validation` branch: Simplified stubs that don't match
- `smr/status-messages` branch: Separate StatusMessage implementation
- `smr/view-change` branch: ViewChangeHandler with its own types
- `smr/quorum` branch: SMR quorum calculations

### Goal
Merge all SMR branches into a single coherent implementation.

### Steps

1. **Analyze each branch's smr/ directory**:
   ```bash
   # List files in each workspace
   ls -la /Users/bloxyosherlutzki/.subtask/workspaces/-Users-bloxyosherlutzki-dev-ssv-scout-src-ssv--6/protocol/v2/qbft/smr/  # quorum
   ls -la /Users/bloxyosherlutzki/.subtask/workspaces/-Users-bloxyosherlutzki-dev-ssv-scout-src-ssv--7/protocol/v2/qbft/smr/  # timeout-certificate  
   ls -la /Users/bloxyosherlutzki/.subtask/workspaces/-Users-bloxyosherlutzki-dev-ssv-scout-src-ssv--8/protocol/v2/qbft/smr/  # status-messages
   ls -la /Users/bloxyosherlutzki/.subtask/workspaces/-Users-bloxyosherlutzki-dev-ssv-scout-src-ssv--9/protocol/v2/qbft/smr/  # view-change
   ls -la /Users/bloxyosherlutzki/.subtask/workspaces/-Users-bloxyosherlutzki-dev-ssv-scout-src-ssv--10/protocol/v2/qbft/smr/ # proposal-validation
   ls -la /Users/bloxyosherlutzki/.subtask/workspaces/-Users-bloxyosherlutzki-dev-ssv-scout-src-ssv--11/protocol/v2/qbft/smr/ # integration
   ```

2. **Create consolidated smr/ package** with these files:
   - `types.go` - Common type aliases (OperatorID, CommitteeMember)
   - `block.go` - Block type from timeout-certificate (has View, Height, Root)
   - `quorum.go` - SMR quorum functions from smr/quorum task
   - `timeout_certificate.go` - From timeout-certificate (full locking logic)
   - `timeout_certificate_test.go` - Tests
   - `status.go` - StatusMessage, StatusCollector from status-messages
   - `status_test.go` - Tests
   - `view_change.go` - TimeoutManager, ViewChangeHandler from view-change
   - `view_change_test.go` - Tests
   - `proposal.go` - SMRProposal, SMRProof using consolidated types
   - `proposal_validation.go` - Validation using real TimeoutCertificate
   - `proposal_validation_test.go` - Tests
   - `config.go` - SMRConfig from integration task

3. **Prefer implementations in this order**:
   - timeout-certificate > proposal-validation (TC has proper locking logic)
   - Use the real TimeoutCertificate.LocksBlock with Condition 1 & 2
   - Keep quorum calculation from smr/quorum

4. **Update imports and fix incompatibilities**:
   - TCProof should use the real TimeoutCertificate from timeout_certificate.go
   - StatusSetProof should use real StatusMessage
   - Remove duplicate type definitions

5. **Run tests**:
   ```bash
   go test ./protocol/v2/qbft/smr/...
   ```

6. **Verify integration with existing code**:
   - Check protocol/v2/qbft/instance/proposal.go integration

### Key Files to Merge

From `smr/timeout-certificate`:
- `timeout_certificate.go` - **PRIMARY** (has full locking logic)
- `block.go` - Block type
- `timeout_certificate_test.go`

From `smr/proposal-validation`:
- `proposal.go` - SMRProposal, but needs updated types
- `proposal_validation.go` - ValidateSMRProposal
- `proposal_validation_test.go`

From `smr/quorum`:
- Quorum calculation functions (check ssvshare.go)

From `smr/status-messages`:
- StatusMessage, StatusCollector

From `smr/view-change`:
- TimeoutManager, ViewChangeHandler

### Success Criteria
- All SMR types are defined once in a single location
- TimeoutCertificate.LocksBlock implements spec Conditions 1 & 2
- All tests pass
- No duplicate type definitions
