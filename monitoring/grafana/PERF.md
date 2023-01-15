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
* **TBD** Operator data (gauge bar, on|off + total rate + effectiveness)

Row 2:
* Submitted roles (time-series, labeled by role type) \
  `ssv_validator_roles_submitted{role=<beacon role>} = <counter>`
* Failed submissions (time-series, labeled by role type) \
  `ssv_validator_roles_failed{role=<beacon role>} = <counter>`

### Attester Role

Row 1:
* Attestion data request duration \
`ssv_beacon_attestation_data_request_duration_seconds{}`
* Attestion full flow duration (w/o attestation data) \
`ssv_beacon_attestation_full_flow_duration_seconds{}`

Row 2:
* Attestation consensus duration \
`ssv_beacon_attestation_consensus_duration_seconds{}`
* Proposal stage duration (seconds) \
`ssv_qbft_instance_stage_duration_seconds{stage,identifier}`

Row 3:
* Prepare stage duration (seconds) \
`ssv_qbft_instance_stage_duration_seconds{stage,identifier}`
* Commit stage duration (seconds) \
`ssv_qbft_instance_stage_duration_seconds{stage,identifier}`

Row 4:
* Signature collection duration (seconds) \
`ssv_beacon_signature_collection_duration_seconds{identifier}`
* Attestation submission duration (seconds) \
`ssv_beacon_attestation_submission_duration_seconds{identifier}`

### Proposer Role

...

### Aggregator Role

...

