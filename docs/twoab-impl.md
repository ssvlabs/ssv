# 2abOBFT implementation — spec deltas

Tracks deltas between [docs/2abOBFT.md](2abOBFT.md) and the [protocol/v2/obft/twoab](../protocol/v2/obft/twoab) implementation (plus the SSV adapter at `protocol/v2/ssv/runner/obft/twoab` once Phase L lands).

Mirrors the format of the now-deleted `docs/obft-impl.md` (used during bare OBFT's faithfulness review, then removed at commit `5adb11ead` once all items resolved): each delta entry has **Spec.** (what the spec says), **Impl.** (what the implementation currently does), **Fix.** (proposed remediation), **Test.** (test to add). Findings accrete as Phases E-L of [docs/2abOBFT-IMPL-PLAN.md](2abOBFT-IMPL-PLAN.md) land; entries are deleted once resolved.

## Status

Through Phase K (consensustest adapter), no spec deltas have surfaced. The impl is faithful to docs/2abOBFT.md at every check-in.

Implementation observations that are spec-consistent but worth noting in case they look surprising to a reader:

- **Variant C cost is visible**: Without a Phase-1 σ_V partial, the
  PartialEquivocation_NaturalRecovery scenario decides at L_1 in 2abOBFT
  (vs L_0 in bare OBFT). The spec acknowledges this — removing σ_V means
  the leader's "head-start" partial isn't available to push V_a's σ-pool
  over qV. Slot still succeeds, just one layer deeper.

- **2ab decides earlier in wall-clock at same RelayCutoff**: Δ_2a + Δ_2b
  at spec-recommended sizing (2 BTT + 2 BTT) means T_commit lands 2 BTT
  earlier than OBFT's. The verdict-pre-broadcast in Phase 2a absorbs
  propagation that OBFT consolidates into a single Phase-2 window after
  T_commit, so 2ab's RoundEndOffset reaches sooner. Spec-aligned per
  §Setting / §Phase 2a.

- **Adapter requires leader self-observation**: Bare OBFT's adapter
  doesn't self-observe the leader's bundle because the Phase-1 σ_V
  partial gets rehydrated cluster-wide via Witnesses in Phase 2. 2ab
  Variant C has no σ_V (and no Witnesses array), so the leader must
  retain their own bundle locally to participate in their own Phase-2a
  verdict. Documented inline in `consensustest/twoab/events.go`'s
  `evtLeaderFetch` handler.

Each delta below would be labelled `D<n>`, numbered in order of
identification (not severity). None as of Phase K.

---

(empty — no deltas surfaced through Phase K)
