---
title: Reduce duty processing log verbosity
base-branch: main
---

## Goal
Reduce log volume from "starting duty processing" and "executing validator duty" logs (currently ~3M logs/hour combined) while preserving duty execution traceability.

## Context
These logs are generated for every duty (attestation, aggregation, sync committee, proposals) at the start of processing. With many validators, this creates massive log volume.

## Requirements
1. Find where "ℹ️ starting duty processing" and "🔧 executing validator duty" are logged
2. Reduce verbosity by ONE of these approaches:
   - Change to TRACE level
   - Consolidate multiple validator duties in same slot into a single log with count
   - Log only at INFO for proposals/sync committee (less frequent), DEBUG/TRACE for attestations
3. Keep duty_id available for correlation
4. Consider batching: "Starting N attestation duties for slot X" instead of N individual logs

## Constraints
- Don't break any tests
- Keep proposal duties visible at DEBUG level (they're less frequent and more critical)
