---
title: Reduce exporter event processing logs
base-branch: main
---

## Goal
Reduce log volume from "processed event", "share not found, created a new one", and "processing event" logs (~1M logs/hour from ssv-node-exporter) while preserving event processing visibility.

## Context
The SSV exporter node processes blockchain events and logs extensively. These logs are mostly informational and create significant volume.

## Requirements
1. Find where these messages are logged in the exporter code:
   - "processed event"
   - "share not found, created a new one"  
   - "processing event"
   - "quorum reached after flush"
2. Reduce verbosity:
   - Move routine "processed event" to TRACE
   - Batch "processed N events from block X" instead of individual logs
   - "share not found, created a new one" could be TRACE or removed if shares are tracked elsewhere
   - Keep "quorum reached after flush" at DEBUG (less frequent, more useful)
3. Consider adding periodic summary logs instead of per-event logs

## Constraints
- Don't break any tests
- Error cases should remain visible
- Keep enough info to debug event processing issues
