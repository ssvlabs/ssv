---
title: SMR Protocol Integration and Testing
base-branch: main
---

## Task: SMR Protocol Integration and Testing

### Background
After implementing all SMR components, they need to be integrated into the SSV
QBFT instance and thoroughly tested together.

### Requirements

1. **Create SMR configuration** in `protocol/v2/qbft/smr/config.go`:
   ```go
   type SMRConfig struct {
       Enabled           bool
       TimeoutDuration   time.Duration  // (2p+2)Δ
       BlocksPerTimeout  int            // p parameter
   }
   
   func DefaultSMRConfig() *SMRConfig
   func (c *SMRConfig) Validate() error
   ```

2. **Update IConfig interface** in `protocol/v2/qbft/config.go`:
   - Keep existing `IsSMRMode() bool`
   - Add `GetSMRConfig() *SMRConfig`
   - Add methods for SMR-specific quorum if needed

3. **Update Instance** in `protocol/v2/qbft/instance/instance.go`:
   - Add SMR-specific state:
     ```go
     smrViewChange    *ViewChangeHandler
     smrHighestTC     *TimeoutCertificate
     ```
   - Initialize SMR components in NewInstance when SMR mode
   - Route messages to appropriate handlers based on mode

4. **Create SMR message router** in `protocol/v2/qbft/smr/router.go`:
   - Route TimeoutMessage to TimeoutManager
   - Route StatusMessage to StatusCollector (leader only)
   - Integrate with existing message processing

5. **Update ProcessMsg** to handle SMR message types:
   - Add cases for TimeoutMsgType, StatusMsgType
   - Use SMR quorum for commit in SMR mode
   - Handle view change via SMR protocol

6. **Integration tests** in `protocol/v2/qbft/smr/integration_test.go`:
   
   **Happy path scenarios**:
   - 4-node committee reaches consensus in 2 rounds (no timeouts)
   - Multiple consecutive blocks in same view
   - View change with TC locking
   
   **Timeout scenarios**:
   - Leader failure triggers timeout and view change
   - TC formation with various locking conditions
   - View change with status messages
   
   **Edge cases**:
   - Conflicting blocks from malicious leader
   - Late messages after view change
   - Network partition and recovery
   
   **Backward compatibility**:
   - QBFT mode still works correctly
   - Mode switching (if supported)

7. **Benchmark tests** in `protocol/v2/qbft/smr/benchmark_test.go`:
   - Compare SMR vs QBFT latency
   - Measure message overhead
   - Test scalability with different committee sizes

8. **Documentation**:
   - Update README or add SMR.md explaining the protocol
   - Document configuration options
   - Add migration guide from QBFT to SMR

### Files to Create/Modify
- `protocol/v2/qbft/smr/config.go` (new)
- `protocol/v2/qbft/smr/router.go` (new)
- `protocol/v2/qbft/smr/integration_test.go` (new)
- `protocol/v2/qbft/smr/benchmark_test.go` (new)
- `protocol/v2/qbft/config.go` (modify)
- `protocol/v2/qbft/instance/instance.go` (modify)

### Dependencies
- Requires all previous smr/* tasks to be complete
- smr/quorum
- smr/timeout-certificate
- smr/status-messages
- smr/view-change
- smr/proposal-validation

### Testing Checklist
- [ ] Unit tests pass for all SMR components
- [ ] Integration tests pass for happy path
- [ ] Integration tests pass for timeout/view change
- [ ] QBFT mode regression tests pass
- [ ] Benchmark shows expected 2-round latency
- [ ] No race conditions (go test -race)
