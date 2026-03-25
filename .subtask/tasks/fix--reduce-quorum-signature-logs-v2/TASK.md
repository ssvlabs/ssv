---
title: Reduce quorum and signature reconstruction logs
base-branch: stage
---

## Goal
Reduce log volume from "got pre-consensus quorum" and "reconstructed partial signature" logs (~3M logs/hour combined) while keeping quorum achievement visible.

## Context
These logs fire when enough partial signatures are collected (quorum) and when the final signature is reconstructed. They're critical for debugging but very high volume.

## Requirements
1. Find where "🎯 got pre-consensus quorum" and "🧩 reconstructed partial signature" are logged
2. Reduce verbosity while keeping the IMPORTANT information:
   - Which duty achieved quorum
   - How many signers contributed
   - Success/failure of reconstruction
3. Approaches to consider:
   - Keep quorum log at DEBUG, move reconstruction to TRACE
   - Combine into single log: "Quorum reached and signature reconstructed for duty X with signers [1,2,3]"
   - Log only failures at DEBUG, successes at TRACE
4. The signers list is important for debugging - preserve it somehow

## Constraints
- Don't break any tests
- Quorum failures should remain visible at DEBUG or higher
