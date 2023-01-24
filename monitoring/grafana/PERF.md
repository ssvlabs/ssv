[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV Monitoring: Operator Performance Dashboard

Shows metrics for validators stats, consensus committees status and health of the SSV node.

See [JSON file](./grafana/dashboard_ssv_performance.json) to import.

The dashboard consists of the following sections:

### Operator Stats

Row 1:
* Validators (gauge bar, off|idle|working|change-round) \
  `ssv_validators_status{mode=off|idle|working} = <counter>` 
and `ssv_qbft_instance_round{identifier} = <counter>`
* Operator data (gauge bar, on|off + total rate + effectiveness) \
  **TBD**

Row 2:
* Submitted roles (time-series, labeled by role type) \
  `ssv_validator_roles_submitted{role=<beacon role>} = <counter>`
* Failed submissions (time-series, labeled by role type) \
  `ssv_validator_roles_failed{role=<beacon role>} = <counter>`

### Attester Role

Row 1:
* Attestation data request duration \
`ssv_beacon_attestation_data_request_duration_seconds{}`
* Attestation full flow duration (w/o attestation data) \
`ssv_beacon_duty_full_flow_duration_seconds{role}`

Row 2:
* Attestation consensus duration \
`ssv_beacon_consensus_duration_seconds{role}`
* Proposal stage duration (seconds) \
`ssv_qbft_instance_stage_duration_seconds{stage,identifier}`

Row 3:
* Prepare stage duration (seconds) \
`ssv_qbft_instance_stage_duration_seconds{stage,identifier}`
* Commit stage duration (seconds) \
`ssv_qbft_instance_stage_duration_seconds{stage,identifier}`

Row 4:
* Post-consensus duration (Signature collection duration) (seconds) \
`ssv_validator_post_consensus_duration_seconds{role,identifier}`
* Attestation submission duration (seconds) \
`ssv_validator_beacon_submission_duration_seconds{role,identifier}`

### Proposer Role

Row 1:
* Duty full flow duration \
  `ssv_beacon_duty_full_flow_duration_seconds{role}`
* Consensus duration \
  `ssv_beacon_consensus_duration_seconds{role}`

Row 2:
* Pre-consensus duration (seconds) \
  `ssv_validator_pre_consensus_duration_seconds{role,identifier}`
* Post-consensus duration (seconds) \
  `ssv_validator_post_consensus_duration_seconds{role,identifier}`

Row 3:
* Block submission duration (seconds) \
  `ssv_validator_beacon_submission_duration_seconds{role,identifier}`

### Aggregator Role

Row 1:
* Duty full flow duration \
  `ssv_beacon_duty_full_flow_duration_seconds{role}`
* Consensus duration \
  `ssv_beacon_consensus_duration_seconds{role}`

Row 2:
* Pre-consensus duration (seconds) \
  `ssv_validator_pre_consensus_duration_seconds{role,identifier}`
* Post-consensus duration (seconds) \
  `ssv_validator_post_consensus_duration_seconds{role,identifier}`

Row 3:
* Proof submission duration (seconds) \
  `ssv_validator_beacon_submission_duration_seconds{role,identifier}`


