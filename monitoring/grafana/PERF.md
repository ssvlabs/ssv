[<img src="../docs/resources/ssv_header_image.png" >](https://www.ssv.network/)

<br>
<br>


# SSV Monitoring: Operator Performance Dashboard

Shows metrics for validators stats, consensus committees status and health of the SSV node.

See [JSON file](./dashboard_ssv_operator_performance.json) to import.

The dashboard consists of the following sections:

### Operator Stats

**Row 1:**
* Validators status: `ssv_validators_status{mode=off|idle|working} = <counter>` (gauge bar)
* **To do**: Operator data (effectiveness)

**Row 2:**

### Attester Role

**Row 1:**
* Instance round distribution: (off|idle|working|change-round): `ssv_qbft_instance_round{identifier} = <counter>` (time-series)
* Failed role submissions: `ssv_validator_roles_failed{role=<beacon role>} = <counter>` (time-series)
* Submitted roles: `ssv_validator_roles_submitted{role=<beacon role>} = <counter>` (time-series)

**Row 2:**
* Duty full flow duration (excluding attestation data request): `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)

**Row 3:**
* Proposal stage duration: `ssv_qbft_instance_stage_duration_seconds{stage,identifier}` (time-series)
* Prepare stage duration: `ssv_qbft_instance_stage_duration_seconds{stage,identifier}` (time-series)
* Commit stage duration: `ssv_qbft_instance_stage_duration_seconds{stage,identifier}` (time-series)

**Row 4:**
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}` (time-series)
* Post-consensus duration (Signature collection duration): `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)

**Row 5:**
* Attestation data request duration: `ssv_beacon_data_request_duration_seconds{role}` (time-series)
* Attestation submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

### Proposer Role

**Row 1:**
* Instance round distribution: (off|idle|working|change-round): `ssv_qbft_instance_round{identifier} = <counter>` (time-series)
* Failed role submissions: `ssv_validator_roles_failed{role=<beacon role>} = <counter>` (time-series)
* Submitted roles: `ssv_validator_roles_submitted{role=<beacon role>} = <counter>` (time-series)

**Row 2:**
* Duty full flow duration: `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)

**Row 3:**
* Beacon block request duration: `ssv_beacon_data_request_duration_seconds{role}` (time-series)
* Block submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

**Row 4:**
* Pre-consensus duration: `ssv_validator_pre_consensus_duration_seconds{role,identifier}` (time-series)
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}`
* Post-consensus duration: `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)

### Aggregator Role

**Row 1:**
* Instance round distribution: (off|idle|working|change-round): `ssv_qbft_instance_round{identifier} = <counter>` (time-series)
* Failed role submissions: `ssv_validator_roles_failed{role=<beacon role>} = <counter>` (time-series)
* Submitted roles: `ssv_validator_roles_submitted{role=<beacon role>} = <counter>` (time-series)

**Row 2:**
* Duty full flow duration (excluding attestation data and aggregate attestation requests): `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)

**Row 3:**
* Aggregate attestation request duration: `ssv_beacon_data_request_duration_seconds{role}` (time-series)
* Proof submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

**Row 4:**
* Pre-consensus duration: `ssv_validator_pre_consensus_duration_seconds{role,identifier}` (time-series)
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}`
* Post-consensus duration: `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)

### Sync Committee Role

**Row 1:**
* Instance round distribution: (off|idle|working|change-round): `ssv_qbft_instance_round{identifier} = <counter>` (time-series)
* Failed role submissions: `ssv_validator_roles_failed{role=<beacon role>} = <counter>` (time-series)
* Submitted roles: `ssv_validator_roles_submitted{role=<beacon role>} = <counter>` (time-series)

**Row 2:**
* Duty full flow duration (excluding beacon block root request): `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)

**Row 3:**
* Beacon block root request duration: `ssv_beacon_data_request_duration_seconds{role}` (time-series)
* Sync message submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

**Row 4:**
* Pre-consensus duration: `ssv_validator_pre_consensus_duration_seconds{role,identifier}` (time-series)
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}`
* Post-consensus duration: `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)

### Sync Committee Aggregator Role

**Row 1:**
* Instance round distribution: (off|idle|working|change-round): `ssv_qbft_instance_round{identifier} = <counter>` (time-series)
* Failed role submissions: `ssv_validator_roles_failed{role=<beacon role>} = <counter>` (time-series)
* Submitted roles: `ssv_validator_roles_submitted{role=<beacon role>} = <counter>` (time-series)

**Row 2:**
* Duty full flow duration (excluding beacon block root and sync committee contribution requests): `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)

**Row 3:**
* Sync committee contribution request duration: `ssv_beacon_data_request_duration_seconds{role}` (time-series)
* Signed contribution and proof submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

**Row 4:**
* Pre-consensus duration: `ssv_validator_pre_consensus_duration_seconds{role,identifier}` (time-series)
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}`
* Post-consensus duration: `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)
