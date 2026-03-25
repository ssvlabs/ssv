---
title: SMR Integration and Alignment Check
base-branch: smr/full-implementation
---

## Task: SMR Integration and Final Alignment

### Context
This task runs AFTER smr/status-v2, smr/view-change-v2, and smr/proposal-v2 are complete.
It ensures all components work together correctly.

### Requirements

1. **Verify type consistency**:
   - All files in `protocol/v2/qbft/smr/` use the same types
   - No duplicate type definitions
   - TimeoutCertificate, Block, OperatorID defined only once

2. **Create `config.go`** for SMR configuration:
   ```go
   type SMRConfig struct {
       Enabled         bool
       TimeoutDuration time.Duration
   }
   
   func DefaultSMRConfig() *SMRConfig
   ```

3. **Update QBFT Instance** (`protocol/v2/qbft/instance/instance.go`):
   - Add SMR state fields when IsSMRMode()
   - Route timeout to SMR TimeoutManager
   - Use SMR quorum for commits

4. **Integration tests** in `integration_test.go`:
   - Happy path: 2-round consensus
   - Timeout and view change
   - Full cycle with TC and status messages

5. **Run ALL tests**:
   ```bash
   go test ./protocol/v2/qbft/...
   go test ./protocol/v2/types/...
   ```

6. **Alignment checklist**:
   - [ ] No duplicate type definitions in smr/
   - [ ] All imports use same package types
   - [ ] TimeoutCertificate.LocksBlock implements spec Conditions 1 & 2
   - [ ] SMR quorum uses 4f-1 formula
   - [ ] All tests pass

### Files to Check
- `protocol/v2/qbft/smr/*.go` - all should be consistent
- `protocol/v2/qbft/instance/*.go` - SMR integration
- `protocol/v2/types/ssvshare.go` - SMR quorum functions
