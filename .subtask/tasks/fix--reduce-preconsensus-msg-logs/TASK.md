---
title: Reduce pre-consensus message log verbosity
base-branch: main
---

## Goal
Reduce log volume from "got pre-consensus message" logs (currently ~5M logs/hour in stage) while preserving ability to debug duty flow.

## Context
These logs are generated when SSV nodes receive pre-consensus partial signature messages during duty execution. They're extremely high volume (5M/hour) and mostly at DEBUG level.

## Requirements
1. Find where "📬 got pre-consensus message" is logged in the codebase
2. Reduce verbosity by ONE of these approaches (pick the best):
   - Change log level from DEBUG to TRACE (if available)
   - Add sampling (log 1 in N messages, with a summary count)
   - Consolidate into a single log per duty showing message counts by signer
   - Remove the log entirely if the information is captured elsewhere
3. Ensure we can still reconstruct which signers sent messages for a duty when debugging
4. The duty_id field must remain available for correlation with other logs

## Constraints
- Don't break any tests
- Preserve enough information to debug consensus issues
- Consider adding a summary log at the end of pre-consensus phase instead
