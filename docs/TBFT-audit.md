Reviewed [`docs/TBFT.md` at `100bbe5`](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md). I focused on TBFT only and did not review TBFTR beyond confirming TBFT now delegates `n >= 7` to it.

**Findings**

- **[P0] The new `σ+NR` exclusion rule is not cryptographic safety.** [TBFT.md:L91-L108](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L91-L108) and [TBFT.md:L153-L168](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L153-L168) rely on honest aggregators excluding cross-signers, but a Byzantine operator can ignore that rule and aggregate valid shares offline. Example: one Byzantine signs primary `V_p` and also emits `NR`; two honest operators sign `V_p`; one honest emits `NR`. That gives `qV=3` for `V_p` and `qEnc=2` to decrypt backup. If the three honest backup partials are present, the Byzantine can also reconstruct `V_b`. Beacon submission validates only the final BLS signature, not the signer set or exclusion rule. This makes the current `qEnc=2` design unsafe unless exclusion is enforced cryptographically or by a downstream-verifiable certificate, which Ethereum block submission does not provide.

- **[P1] Phase-1 V-share signing is under-specified for slashing protection.** [TBFT.md:L35-L38](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L35-L38) and [TBFT.md:L46-L46](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L46-L46) require leaders to sign candidates with the real V-share before consensus, but [TBFT.md:L147-L147](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L147-L147) only calls out Phase 2 onion signing. EKM/slashing policy must explicitly cover Phase 1 leader signatures too, including backup refreshes.

- **[P1] Backup refresh conflicts with equivocation handling.** [TBFT.md:L40-L40](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L40-L40) says `L_b` should refresh and re-broadcast if the head changes, while [TBFT.md:L64-L67](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L64-L67) says two distinct signed candidates from the same leader are equivocation. The doc needs a supersession rule: when a new-head candidate replaces a stale-head candidate, what metadata proves it is a legitimate refresh rather than slashable equivocation?

- **[P2] Candidate authentication is improved, but the signed payload should bind context.** [TBFT.md:L52-L58](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L52-L58) now requires leader identity and V-share checks. The operator-identity signature should sign a structured envelope, not just `V`: protocol version, cluster, slot, role/layer, leader id, and value root. Otherwise replay or role confusion has to be ruled out indirectly by application validity.

- **[P2] “No in-bound miss scenarios” is still too strong.** [TBFT.md:L172-L190](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L172-L190) correctly says TBFT has no termination guarantee, but [TBFT-comparison.md:L49-L50](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT-comparison.md#L49-L50) says there are no in-bound misses. With one Byzantine backup leader silent and one honest operator missing an honest primary before `T_commit`, the primary can fail to reach `qV` and backup can be unavailable. This is synchrony-dependent, not just “more than f failures.”

**Previous Feedback Status**

- **Fully addressed: TBFT is now scoped to `n=4`.** [TBFT.md:L1-L5](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L1-L5) cleanly moves larger clusters to TBFTR, so my old `n=7+` TBFT fallback critique is no longer directly in TBFT scope.

- **Fully addressed: tag indexing ambiguity is gone.** [TBFT.md:L24-L24](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L24-L24) and [TBFT.md:L73-L83](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L73-L83) use one clear 0-based primary-to-backup tag.

- **Fully addressed: final-certificate gossip is specified.** [TBFT.md:L117-L123](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L117-L123) covers the lone-reconstructor submission failure I called out.

- **Mostly addressed: application validity is now explicit.** [TBFT.md:L131-L147](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L131-L147) is a strong improvement. Remaining gap: include Phase 1 leader V-share signing in the same slashing-protection discussion.

- **Mostly addressed: “at most one full sig” is now scoped per instance.** [TBFT.md:L276-L280](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L276-L280) addresses the wording issue. The underlying safety claim remains blocked by the `qEnc=2` cross-signing problem above.

- **Partially addressed: selective-delivery liveness is improved but not safe as written.** [TBFT.md:L174-L188](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L174-L188) closes the old no-quorum table if Byzantine actors do not cross-sign. In a Byzantine model, cross-signing must be assumed, and with `qEnc=2` it becomes a safety issue.

**Cleanup Notes**

- Define `T_arrival` in [TBFT.md:L260-L260](https://github.com/ssvlabs/ssv/blob/100bbe529ea931bb11a912154ecc38c1dd148367/docs/TBFT.md#L260-L260), since it is not obvious whether it refers to Phase 1 candidate arrival, onion arrival, or both.

- State explicitly that duplicate shares from the same operator are counted once, especially for the leader’s Phase 1 V-share plus its Phase 2 onion share.