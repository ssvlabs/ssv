
# Doppelganger Protection in `ssv.network`

## 1. Introduction to Doppelganger Protection
Doppelganger (DG) protection is a security mechanism designed to **prevent a validator from accidentally running in two places at the same time.** This is critical in **Proof-of-Stake (PoS) networks** like Ethereum, where **double signing** can lead to **slashing penalties**.

### How Doppelganger Detection Works in Most Validator Clients (e.g., Lighthouse, Prysm)
Most validator clients, including **Lighthouse, Prysm, Teku, and Nimbus**, implement Doppelganger detection by:

1. **Checking validator activity**: The client queries the Ethereum network to check whether a validator has been recently active (i.e., seen in attestations network messages).
2. **If active, the client is terminated**: When a validator is found to be live elsewhere, **the client stops performing duties for all validators running on the same client**, preventing accidental slashing.
3. **If inactive, the validator temporarily disables signing**: The validator continues checking for activity until **a predefined number of epochs** pass without detection.

## 2. How is Doppelganger Protection Different in `ssv.network`?
In `ssv.network`, validators operate under a **distributed multi-operator model**, where **a committee of operators jointly signs** instead of a single validator client.
As a result, traditional Doppelganger detection methods used by solo staking clients need to be adapted to account for the distributed nature of signing.

### When is a Validator Marked as Safe in `ssv.network`?
A validator is **considered safe to sign** when:

1. **It has not been active on the Ethereum network for a predefined number of epochs**. If no recent attestations/proposals are found, the validator is **safe to sign**.
2. **Post-consensus quorum confirms validator safety**. If a validator participates in consensus and reaches the post-consensus step (i.e., a majority of operators in the validator’s committee agree via post-consensus partial messages quorum aka "decided"), then the validator can be marked as **safe** and start signing duties.
    - **Rationale**: This guarantees that the validator is **already actively running inside its `ssv.network` committee**, ensuring that the liveness status reported by the Beacon Node is likely due to its participation in `ssv.network` rather than another client.

## 3. Pros & Cons of `ssv.network` Doppelganger Protection

### 🏆 **How ssv.network Doppelganger Protection Differs from Solo Staking Clients**

| 🟢 **Pros (ssv.network DG)** | 🔴 **Cons (ssv.network DG)** | 🏛️ **Solo Staking Clients (Lighthouse, Prysm, Teku, etc.)** |
|-----------------------------|-----------------------------|------------------------------------------------|
| ✅ **Prevents slashing** by ensuring validators do not sign while potentially active elsewhere. | ⚠️ **Operators running beacon nodes must enable liveness tracking** for Doppelganger protection to function properly. Example: Teku requires `--beacon-liveness-tracking-enabled=true`. | ❌ **Terminates the entire validator client process** upon detecting a Doppelganger, preventing further duties for all validators on that node. |
| ⚡ **Minimizes validator downtime** by allowing a **post-consensus quorum** to determine safety, instead of waiting blindly for inactivity epochs. | 🏗️ **Higher state-tracking complexity** due to integration with post-consensus logic. | ⏳ **Disables signing for all validators** and waits for a predefined inactivity period. |
| 🤝 **Works in distributed staking setups** where multiple operators sign jointly. **Each validator is evaluated separately.** If the BN fails to report accurate liveness, the validator can still be marked safe via **post-consensus operator agreement.** | ⏳ **Potential delays** if the post-consensus quorum does not finalize in time. ⚙️ **Extra computational overhead**, as each validator's state must be tracked independently. | 📡 **Solo staking clients fully depend on BN-reported liveness**, which can be inaccurate due to network issues. A single **faulty BN report** can cause unnecessary downtime. |
| 🎯 **Granular control**: Instead of disabling all validators at once, **validators can be marked safe individually** when meeting safety criteria. | 🔍 **One or more operators failing to enable BN liveness tracking could affect accurate Doppelganger detection.** | ⚡ **One faulty BN report can lead to unnecessary downtime for all validators on the client.** |

# 4. Addiotional considerations
### 💰 Cost of Protection
- A validator will incur penalties due to 2-3 missed epochs during Doppelganger Protection.
- This trade-off is minor compared to the potential slashing penalties, making it an acceptable cost for securing validators.

### 🛑 Imperfect Nature of Doppelganger Protection
- Doppelganger detection relies on network communication and message propagation, which can fail under certain circumstances.
- The design must handle network latency and faults gracefully to avoid unintended validator downtime.

# 5. Implementation in `ssv.network`
The implementation follows a **modular and extendable design**, with:
- **`doppelganger.Provider` interface**: Defines required DG operations.
    ```go
    type Provider interface {
        ValidatorStatus(validatorIndex phase0.ValidatorIndex) Status
        StartMonitoring(ctx context.Context)
        MarkAsSafe(validatorIndex phase0.ValidatorIndex)
        RemoveValidatorState(validatorIndex phase0.ValidatorIndex)
    }
    ```
- **`doppelgangerHandler` struct**: Implements liveness tracking and post-consensus validation.
- **`NoOpDoppelgangerHandler`**: A **noop implementation** when the feature is disabled.
- **`doppelgangerState` struct**: Tracks each validator’s safety status.
    ```go
    type doppelgangerState struct {
        remainingEpochs uint64 // The number of epochs that must be checked before it's considered safe.
    }
    ```
- **Beacon node integration**: Uses `ValidatorLiveness()` API for detection.
- **Post-consensus safety checks**: Validators may be marked safe based on post-consensus quorum instead of waiting full epochs.

## Key Changes in Codebase

### 1. Beacon Node Client
- Updated `goclient.MultiClient` to implement `ValidatorLivenessProvider`.
- Added `ValidatorLiveness()` method to `goclient`.

### 2. CLI Configuration (`cli/operator/node.go`)
- Added a **CLI flag** to enable or disable Doppelganger protection:
    ```go
      EnableDoppelgangerProtection bool `yaml:"EnableDoppelgangerProtection" env:"ENABLE_DOPPELGANGER_PROTECTION" env-description:"Flag to enable Doppelganger protection for validators."
    ```

- If disabled, a `NoOpDoppelgangerHandler` is used.

### 3. Validator State Tracking (`doppelganger/doppelganger_state.go`)

- Introduced constants:
    ```go
    // DefaultRemainingDetectionEpochs represents the initial number of epochs
    // a validator must pass without liveness detection before being considered safe to sign.
    const DefaultRemainingDetectionEpochs uint64 = 2

    // PermanentlyUnsafe is a special flag value used to mark a validator as permanently unsafe for signing.
    // It indicates that the validator was detected as live on another node and should not be trusted for signing.
    const PermanentlyUnsafe = ^uint64(0)
    ```

- Refined safety check logic:
    ```go
    // If previously marked as permanently unsafe but now inactive,  
    // reset detection with one less epoch to ensure revalidation.  
    if state.remainingEpochs == PermanentlyUnsafe {
        state.remainingEpochs = DefaultRemainingDetectionEpochs - 1
    }
    ```


### 4. Doppelganger Service (`doppelganger/doppelganger.go`)
- ![DG monitoring cycle](https://hackmd.io/_uploads/H1hCYQG9kg.png)

- 🔄 Liveness Monitoring & State Tracking
  The Doppelganger Monitoring Process is a crucial mechanism that runs every slot to keep the Doppelganger state map (doppelgangerState) up-to-date.

    1. The `doppelgangerHandler` continuously monitors validators by running every slot.
    2. it queries the `ValidatorProvider` to fetch currently participating validators for the operator.
    3. The `Doppelganger` state map is updated accordingly.
    ```go
    doppelgangerState:  make(map[phase0.ValidatorIndex]*doppelgangerState)
    ```
    4. The map acts as the backbone for the DG protection mechanism, meaning
       If a validator is not found in the map, it is not safe to sign

    ```go
    func (ds *doppelgangerHandler) StartMonitoring(ctx context.Context) {
      ds.logger.Info("Doppelganger monitoring started")
      ticker := ds.slotTickerProvider()

      for {
          select {
          case <-ctx.Done():
              return
          case <-ticker.Next():
              ds.checkLiveness(ctx)
          }
      }
    }
    ```

- **Liveness check before allowing signing:**
    ```go
    func (ds *doppelgangerHandler) checkLiveness(ctx context.Context) {
        ds.mu.Lock()
        validatorsToCheck := make([]phase0.ValidatorIndex, 0, len(ds.doppelgangerState))
        for validatorIndex, state := range ds.doppelgangerState {
            if state.requiresFurtherChecks() {
                validatorsToCheck = append(validatorsToCheck, validatorIndex)
            }
        }
        ds.mu.Unlock()

        if len(validatorsToCheck) == 0 {
            ds.logger.Debug("No validators require liveness check")
            return
        }

        livenessData, err := ds.beaconNode.ValidatorLiveness(ctx, epoch, validatorsToCheck)
        if err != nil {
            ds.logger.Error("Failed to obtain validator liveness data", zap.Error(err))
            return
        }

        ds.processLivenessData(livenessData)
    }
    ```

- **Processing Liveness Data in doppelgangerHandler**
  One of the most critical parts of Doppelganger Protection in `ssv.network` is the **processing of validator liveness data**. This ensures that validators transition safely from a potentially unsafe state to a safe signing state.

    ```go
    for _, response := range livenessData {
        state, exists := ds.doppelgangerState[response.Index]
        if !exists {
            ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(response.Index))
            continue
        }

        if response.IsLive {
            ds.logger.Warn("Doppelganger detected!", fields.ValidatorIndex(response.Index))
            state.remainingEpochs = PermanentlyUnsafe
            continue
        }

        // Reset detection if previously unsafe but now inactive
        if state.remainingEpochs == PermanentlyUnsafe {
            state.remainingEpochs = DefaultRemainingDetectionEpochs - 1
            ds.logger.Debug("Validator requires further checks", fields.ValidatorIndex(response.Index))
            continue
        }

        // Decrease the detection period until the validator is safe to sign again
        state.decreaseRemainingEpochs()
        if state.requiresFurtherChecks() {
            ds.logger.Debug("Validator still requires further checks", fields.ValidatorIndex(response.Index))
        } else {
            ds.logger.Debug("Validator is now safe to sign", fields.ValidatorIndex(response.Index))
        }
    }
    ```



### 5. Event Handling (`eth/eventhandler/handlers.go`)
- **Removes validators from the state when they are removed/liquidated:**
    ```go
    func (eh *EventHandler) handleValidatorRemoved(txn basedb.Txn, event *contract.ClusterValidatorRemoved)   {
      eh.doppelgangerHandler.RemoveValidatorState(event.ValidatorIndex)
    }

    func (eh *EventHandler) handleClusterLiquidated(txn basedb.Txn, event *contract.ClusterLiquidated) {
      for _, share := range toLiquidate {
          eh.doppelgangerHandler.RemoveValidatorState(share.ValidatorIndex)
      }
    }
    ```

### 6. Committee/Proposer Runners (`protocol/v2/ssv/runner/committee.go(proposer.go)`)

- 🚫 Prevents attestation/proposal signing if Doppelganger check is pending
    ```go
    if cr.doppelgangerHandler.ValidatorStatus(duty.ValidatorIndex) != doppelganger.SigningEnabled {
        logger.Warn("Doppelganger check in progress, signing not permitted", fields.ValidatorIndex(duty.ValidatorIndex))
        continue
    }
    ```

- ✅ Marks validator as safe after post-consensus partial messages quorum
    ```go
    if cr.doppelgangerHandler.ValidatorStatus(validator) == doppelganger.SigningDisabled {
        cr.doppelgangerHandler.MarkAsSafe(validator)
    }
    ```

# 6. Doppelganger Protection Log Guide

| 🏷️ **Event** | 🔍 **Description** | 📜 **Example Log** |
|-------------|------------------|-------------------|
| **🛡️ Doppelganger Protection Status** | Logs when **Doppelganger protection** is **enabled or disabled** at startup. | `[INFO] Doppelganger protection enabled.` / `[INFO] Doppelganger protection disabled.` |
| **🟢 Doppelganger Monitoring Started** | Logs when the monitoring loop starts | `[INFO] Doppelganger monitoring started` |
| **🛠 Ticker Event** | Logs epoch & slot position for tracking | `[DEBUG] 🛠 ticker event epoch_slot_pos="e114562-s3665985-#2"` |
| **🆕 Validator Added to State** | Logs when a new validator is tracked | `[DEBUG] Added validator to Doppelganger state validator_index=123` |
| **🗑️ Validator Removed from State** | Logs when a validator is removed | `[DEBUG] Removing validator from Doppelganger state validator_index=123` |
| **🚫 No Validators for Liveness Check** | Logs when no validators need checking | `[DEBUG] No validators require liveness check` |
| **⚠️ Failed Liveness Check** | Logs when Beacon Node fails to provide liveness data | `[ERROR] Failed to obtain validator liveness data error="beacon node request failed"` |
| **🔎 Processing Liveness Data** | Logs when liveness check results are processed | `[DEBUG] Processing liveness data epoch=420 validator_index=123 is_live=true` |
| **🛑 Doppelganger Detected** | Logs when a validator is active elsewhere | `[WARN] Doppelganger detected for validator validator_index=123 epoch=4567` |
| **🟡 Validator No Longer Live, Requires Further Checks** | Logs when a validator was previously unsafe but is now inactive and under observation again. | `[DEBUG] Validator is no longer live but requires further checks validator_index=123 remaining_epochs=1` |
| **🟠 Validator Still Under Observation** | Logs when a validator is still under observation and requires more epochs to be marked safe. | `[DEBUG] Validator still requires further checks validator_index=123 remaining_epochs=1` |
| **✅ Validator Marked as Safe** | Logs when a validator is cleared to sign | `[DEBUG] Validator is now safe to sign validator_index=123` |
| **✅ Post-Consensus Safe Confirmation** | Logs when post-consensus **marks a validator as safe** after quorum agreement. | `[DEBUG] mark validator as doppelganger safe validator_index=123` |
| **⛔ Signing Blocked Due to DG Check** | Logs when a validator tries to sign but is still in DG check | `[WARN] Doppelganger check in progress, signing not permitted validator_index=123` |


# 7. Summary & Final Thoughts
This implementation ensures **security, flexibility, and efficient liveness tracking** while **minimizing validator downtime**. Unlike traditional **solo validator clients**, `ssv.network` leverages **post-consensus quorum** to **safely mark validators as active**, reducing unnecessary waiting time.

This is a **significant enhancement** over traditional **Ethereum staking models** and provides **a secure, distributed mechanism for slashing prevention** while keeping validators online efficiently. 🚀
