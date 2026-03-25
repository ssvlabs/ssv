---
title: Implement Status Messages for SMR View Change
base-branch: main
---

## Task: Implement Status Messages for SMR View Change

### Background
In (5f-1)-SMR, when entering a new view, replicas send status messages to the new leader.
The leader uses these to determine what block to propose.

### Spec Reference (Section 5.2)

**Status Message**: `⟨status, w-1, C, T_high⟩_i` sent to new leader L_w where:
- `w-1`: the previous view number
- `C`: QC (Quorum Certificate) for the parent of the block locked by T_high
- `T_high`: the highest Timeout Certificate known to the replica

**Entering New View** (replica collects timeout messages from view w-1):
1. Wait for 4f-1 valid timeout messages that either:
   - (a) contain no conflicting blocks signed by L_{w-1}, OR
   - (b) come from replicas other than L_{w-1}
2. Attempt to form TC from these messages, update local T_high
3. Enter view w and send status message to new leader L_w

### Requirements

1. **Create Status message type** in `protocol/v2/qbft/smr/status.go`:
   ```go
   type StatusMessage struct {
       PreviousView uint64
       QC           *QuorumCertificate  // QC for parent of locked block
       HighestTC    *TimeoutCertificate // highest TC known
       SignerID     OperatorID
       Signature    []byte
   }
   ```

2. **Implement Status message methods**:
   - `NewStatusMessage(prevView uint64, qc *QC, tc *TC, signer OperatorID) *StatusMessage`
   - `(s *StatusMessage) Validate(committee []*Operator) error`
   - `(s *StatusMessage) Sign(signer OperatorSigner) error`
   - `(s *StatusMessage) VerifySignature(committee []*Operator) error`

3. **Create StatusCollector** for leader to collect status messages:
   ```go
   type StatusCollector struct {
       View     uint64
       Messages map[OperatorID]*StatusMessage
   }
   ```
   
   Methods:
   - `NewStatusCollector(view uint64) *StatusCollector`
   - `(sc *StatusCollector) AddMessage(msg *StatusMessage) error`
   - `(sc *StatusCollector) HasQuorum(cm *CommitteeMember) bool` - 4f-1 messages
   - `(sc *StatusCollector) GetHighestTC() *TimeoutCertificate`
   - `(sc *StatusCollector) DetermineStartingBlock() (*Block, interface{})` - returns block and proof (TC or status set)

4. **Implement leader initialization logic** (spec section 5.3):
   - Case A: If any valid TC in status messages locks a block B', use B' with TC as proof
   - Case B: Otherwise, use block locked by highest TC found within status messages

5. **Add validation**:
   - Verify status message signatures
   - Check view number consistency
   - Validate QC and TC within status messages
   - Prevent duplicate signers

6. **Add tests** in `status_test.go`:
   - Status message creation and validation
   - StatusCollector quorum detection
   - Starting block determination (Case A and Case B)
   - Edge cases

### Files to Create
- `protocol/v2/qbft/smr/status.go`
- `protocol/v2/qbft/smr/status_test.go`

### Dependencies
- Requires TimeoutCertificate from task smr/timeout-certificate
- Requires QuorumCertificate (may need to create or use existing commit aggregation)
