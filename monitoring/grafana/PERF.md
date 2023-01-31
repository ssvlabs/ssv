[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV Monitoring: Operator Performance Dashboard

Shows metrics for validators stats, consensus committees status and health of the SSV node.

See [JSON file](./grafana/dashboard_ssv_performance.json) to import.

The dashboard consists of the following sections:

### Operator Stats

**Row 1:**
* Validators status: `ssv_validators_status{mode=off|idle|working} = <counter>` (gauge bar)
* Instance round distribution: (off|idle|working|change-round): `ssv_qbft_instance_round{identifier} = <counter>` (time-series)
* **To do**: Operator data (effectiveness)

**Row 2:**
* Submitted roles: `ssv_validator_roles_submitted{role=<beacon role>} = <counter>` (time-series)
* Failed submissions: `ssv_validator_roles_failed{role=<beacon role>} = <counter>` (time-series)

### Attester Role

**Row 1:**
* Attestation data request duration: `ssv_beacon_attestation_data_request_duration_seconds{}` (time-series)
* Duty full flow duration (without attestation data): `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)

**Row 2:**
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}` (time-series)
* Proposal stage duration: `ssv_qbft_instance_stage_duration_seconds{stage,identifier}` (time-series)

**Row 3:**
* Prepare stage duration: `ssv_qbft_instance_stage_duration_seconds{stage,identifier}` (time-series)
* Commit stage duration: `ssv_qbft_instance_stage_duration_seconds{stage,identifier}` (time-series)

**Row 4:**
* Post-consensus duration (Signature collection duration): `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)
* Attestation submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

### Proposer Role

**Row 1:**
* Duty full flow duration: `ssv_beacon_duty_full_flow_duration_seconds{role}`
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}`

**Row 2:**
* Pre-consensus duration: `ssv_validator_pre_consensus_duration_seconds{role,identifier}` (time-series)
* Post-consensus duration: `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)

**Row 3:**
* Block submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)

### Aggregator Role

**Row 1:**
* Duty full flow duration `ssv_beacon_duty_full_flow_duration_seconds{role}` (time-series)
* Consensus duration: `ssv_beacon_consensus_duration_seconds{role}` (time-series)

**Row 2:**
* Pre-consensus duration: `ssv_validator_pre_consensus_duration_seconds{role,identifier}` (time-series)
* Post-consensus duration: `ssv_validator_post_consensus_duration_seconds{role,identifier}` (time-series)

**Row 3:**
* Proof submission duration: `ssv_validator_beacon_submission_duration_seconds{role,identifier}` (time-series)
