**Findings**

::code-comment{title="[P0] Deeper-layer gates are not cumulative" body="`enc_tag_k = nr_tag_{k-1}` is not safe for K >= 3. Example at n=7/K=3: layer 0 can reach a valid σ quorum, layer 1 can miss and produce an `NR_1` quorum, and then a Byzantine offline aggregator can use `NR_1` to decrypt layer-2 honest σ partials and reconstruct a second full signature. Layer k must require all prior fallthrough proofs, e.g. an AND/cumulative gate over `nr_tag_0..nr_tag_{k-1}` via nested encryption or an equivalent composite witness, and Phase 3’s proof should become a cross-layer induction." file="/Users/iurii/work/ssv/docs/TBFTR.md" start=66 end=66 priority=0 confidence=0.94}

::code-comment{title="[P2] Hash-variant bandwidth is still mixed with full-V liveness" body="The comparison/recommendation still quotes hash-variant bandwidth while crediting TBFTR with the secondary marginal-synchrony closure. The updated TBFTR spec correctly says the hash variant disables peer-onion V recovery, so the comparison should either split rows into `full-V` vs `hash` modes or label the marginal-synchrony success claims as full-V-only with different bandwidth numbers." file="/Users/iurii/work/ssv/docs/TBFT-comparison.md" start=122 end=124 priority=2 confidence=0.84}

**Previous Feedback Status**

Fully addressed: plain late σ bypass. Late σ for k > 0 is now encrypted under the same layer gate as onion σ, and Phase 3 decrypts both sources uniformly. See [docs/TBFTR.md](/Users/iurii/work/ssv/docs/TBFTR.md:79).

Fully addressed: last-layer NR inconsistency. NR is now only for `0..K-2`, and last-layer failure is terminal. See [docs/TBFTR.md](/Users/iurii/work/ssv/docs/TBFTR.md:80) and [docs/TBFTR.md](/Users/iurii/work/ssv/docs/TBFTR.md:108).

Mostly addressed: hash variant recovery proof. The spec now explicitly says the hash variant disables secondary liveness. What remains is the comparison/recommendation wording above.

Addressed: cutoff vs Phase-2 recovery conflict. The liveness section now cleanly separates primary closure under partial synchrony from secondary closure in marginal synchrony. See [docs/TBFTR.md](/Users/iurii/work/ssv/docs/TBFTR.md:192).

**Main New Issue**

The adjacent-only lock is the big blocker. The old K=2 reasoning does not generalize to K=3+. For fallback-priority safety, a deeper layer must be usable only after every higher-priority layer has an NR quorum, not just after the immediately previous layer fails.

No tests run; this was a docs/protocol audit only.