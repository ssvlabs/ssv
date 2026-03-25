---
title: Update View Change Protocol for SMR
base-branch: main
---

## Task: Update View Change Protocol for (5f-1)-SMR

### Background
The current SSV implementation uses QBFT RoundChange messages for view changes.
(5f-1)-SMR uses a different protocol with Timeout messages, TCs, and Status messages.

### Spec Reference

**Section 5.1 - Timeout Generation**:
- Trigger: If less than p blocks committed within (2p+2)Δ time in view w
- Action: Stop voting for view w, multicast timeout message:
  `⟨timeout, ⟨B_highest, w⟩_{L_w}⟩_i`
  where B_highest is highest block voted for in view w (or ⊥ if none)

**Section 5.2 - Entering New View**:
1. Collect 4f-1 valid timeout messages from view w-1 that either:
   - (a) contain no conflicting blocks signed by L_{w-1}, OR
   - (b) come from replicas other than L_{w-1}
2. Form TC from messages, update local T_high
3. Send status message to new leader L_w

**Section 5.3 - Leader Initialization**:
Upon receiving 4f-1 valid status messages:
- Case A: If any valid TC locks a block B', propose B' with proof S=TC
- Case B: Otherwise, set S = status messages, propose block from highest TC in S

### Requirements

1. **Create TimeoutManager** in `protocol/v2/qbft/smr/timeout_manager.go`:
   ```go
   type TimeoutManager struct {
       View            uint64
       HighestVotedBlock *Block
       TimeoutMessages map[OperatorID]*TimeoutMessage
       HighestTC       *TimeoutCertificate
   }
   ```
   
   Methods:
   - `(tm *TimeoutManager) GenerateTimeout(view uint64, highestBlock *Block) *TimeoutMessage`
   - `(tm *TimeoutManager) AddTimeoutMessage(msg *TimeoutMessage) error`
   - `(tm *TimeoutManager) CanEnterNewView(prevLeader OperatorID) bool`
   - `(tm *TimeoutManager) FormTC() *TimeoutCertificate`
   - `(tm *TimeoutManager) GetHighestTC() *TimeoutCertificate`

2. **Create ViewChangeHandler** in `protocol/v2/qbft/smr/view_change.go`:
   ```go
   type ViewChangeHandler struct {
       CurrentView     uint64
       TimeoutManager  *TimeoutManager
       StatusCollector *StatusCollector
       Config          ViewChangeConfig
   }
   ```
   
   Methods:
   - `(vc *ViewChangeHandler) OnTimeout()` - trigger timeout, broadcast timeout msg
   - `(vc *ViewChangeHandler) OnTimeoutMessage(msg *TimeoutMessage) error`
   - `(vc *ViewChangeHandler) TryEnterNewView() (entered bool, err error)`
   - `(vc *ViewChangeHandler) OnStatusMessage(msg *StatusMessage) error` (for leader)
   - `(vc *ViewChangeHandler) GetProposalJustification() (*Block, interface{}, error)` (for leader)

3. **Implement timeout validation** (spec section 5.2 threshold):
   - Check for 4f-1 valid timeout messages
   - Validate no conflicting blocks from previous leader OR messages not from leader
   - This is critical for safety

4. **Integrate with existing Instance**:
   - Add `viewChangeHandler *ViewChangeHandler` field to Instance
   - Modify timeout handling to use SMR protocol when IsSMRMode()
   - Keep existing RoundChange logic for QBFT mode

5. **Update round timer integration**:
   - Trigger SMR timeout generation on timer expiry
   - Configure timeout duration based on spec (2p+2)Δ

6. **Add tests** in `view_change_test.go`:
   - Timeout generation and broadcasting
   - TC formation from timeout messages
   - View entry conditions (both (a) and (b))
   - Leader proposal justification (Case A and B)
   - Integration with existing timer

### Files to Create/Modify
- `protocol/v2/qbft/smr/timeout_manager.go` (new)
- `protocol/v2/qbft/smr/view_change.go` (new)
- `protocol/v2/qbft/smr/view_change_test.go` (new)
- `protocol/v2/qbft/instance/instance.go` (modify for integration)

### Dependencies
- Requires smr/timeout-certificate
- Requires smr/status-messages
- Requires smr/quorum
