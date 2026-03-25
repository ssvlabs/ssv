---
title: Reduce attestation and aggregator signing logs
base-branch: stage
---

## Goal
Reduce log volume from "signed attestation data" and "signed aggregator selection proof" logs (~1.7M logs/hour combined) while preserving signing audit trail.

## Context
These logs fire when validators sign attestations and aggregator selection proofs. They're important for debugging signing issues but generate high volume.

## Requirements
1. Find where "signed attestation data" and "signed aggregator selection proof" are logged
2. Reduce verbosity while keeping audit capability:
   - Move to TRACE level (if signing succeeds)
   - Keep failures at DEBUG or WARN
   - Consider batching: "Signed N attestations for slot X" 
3. Important fields to preserve: validator pubkey, slot, duty_id
4. Signing failures should remain highly visible

## Constraints
- Don't break any tests
- Signing failures must remain at DEBUG or higher
- Consider that these logs help audit what was signed (security relevant)
