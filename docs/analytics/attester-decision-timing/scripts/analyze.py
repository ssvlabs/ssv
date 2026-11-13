#!/usr/bin/env python3
"""Reproduce the numbers in ../README.md from the collected dataset (attest.jsonl).

Usage:  python analyze.py [path/to/attest.jsonl]

Covers: decision-time distribution & thresholds, decided-round distribution,
outcome (inclusion / head-vote / optimal-inclusion) by decision-time band and by
round, and the round-1 consensus-duration distribution. All rates here are
conditional on the committee reaching a decided trace; the overall E2M miss rate
(non-decisions, timing-independent) is stated separately in the README.
"""
import sys, os, json, math
from collections import Counter

DATA = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "data", "attest.jsonl")
rows = [json.loads(l) for l in open(DATA)]
dec = [r for r in rows if r["decided_ms"] is not None]


def pct(a, p):
    a = sorted(a); k = (len(a) - 1) * p / 100; lo, hi = math.floor(k), math.ceil(k)
    return a[int(k)] if lo == hi else a[lo] * (hi - k) + a[hi] * (k - lo)


tv = sum(r["n_val"] for r in rows); ti = sum(r["n_incl"] for r in rows)
th = sum(r["n_head"] for r in rows); topt = sum(r["n_opt"] for r in rows)
print(f"committee-duties: {len(rows)} ({len(dec)} decided)  validator-attestations: {tv:,}")
print(f"decided-attestation inclusion {100*ti/tv:.3f}%  head-vote(of incl) {100*th/ti:.3f}%  optimal-incl {100*topt/ti:.3f}%")

dms = [r["decided_ms"] for r in dec]
print("\ndecision-into-slot percentiles:", {p: round(pct(dms, p)) for p in (50, 75, 90, 95, 99)})
for t in (3000, 4000, 5000, 6000):
    k = sum(1 for x in dms if x > t)
    print(f"  decide > {t/1000:.0f}s: {k} ({100*k/len(dms):.3f}%)")

rd = Counter(r["decided_round"] for r in dec)
print("\ndecided-round dist:", dict(sorted(rd.items(), key=lambda x: (x[0] is None, x[0]))))

print("\noutcome by decision-time band (validator-weighted):")
bins = [(0, 2000, "<2s"), (2000, 3000, "2-3s"), (3000, 4000, "3-4s"), (4000, 5000, "4-5s"), (5000, 6000, "5-6s"), (6000, 1e12, ">6s")]
print(f"  {'band':6} {'duties':>7} {'attest':>9} {'incl%':>8} {'head%':>8} {'optimal%':>9}")
for lo, hi, lbl in bins:
    g = [r for r in dec if lo <= r["decided_ms"] < hi]
    nv = sum(r["n_val"] for r in g); ni = sum(r["n_incl"] for r in g)
    nh = sum(r["n_head"] for r in g); no = sum(r["n_opt"] for r in g)
    if nv:
        print(f"  {lbl:6} {len(g):>7} {nv:>9,} {100*ni/nv:>7.3f}% {100*nh/max(1,ni):>7.2f}% {100*no/max(1,ni):>8.2f}%")

print("\noutcome by decided round (validator-weighted):")
for rk in (1, 2):
    g = [r for r in dec if r["decided_round"] == rk]
    nv = sum(r["n_val"] for r in g); ni = sum(r["n_incl"] for r in g); nh = sum(r["n_head"] for r in g)
    if nv:
        print(f"  round {rk}: {len(g)} duties, {nv:,} attest | incl {100*ni/nv:.3f}% | head {100*nh/ni:.2f}%")

durs = sorted(r["decided_ms"] - r["r1_prop_ms"] for r in dec
              if r["decided_round"] == 1 and r["r1_prop_ms"] is not None and r["decided_ms"] - r["r1_prop_ms"] >= 0)
if durs:
    print(f"\nround-1 duration (n={len(durs)}): median={pct(durs,50):.0f} p90={pct(durs,90):.0f} "
          f"p95={pct(durs,95):.0f} p99={pct(durs,99):.0f} p99.9={pct(durs,99.9):.0f} max={durs[-1]:.0f}ms")
    starts = [r["r1_prop_ms"] for r in dec if r["decided_round"] == 1 and r["r1_prop_ms"] is not None]
    print(f"round-1 start (proposal) into slot: median={pct(starts,50):.0f} p90={pct(starts,90):.0f} p99={pct(starts,99):.0f}ms")

# round-change concentration
rc = [r for r in dec if (r["decided_round"] or 1) > 1]
tot_by_comm = Counter(r["committee_hash"] for r in dec)
rc_by_comm = Counter(r["committee_hash"] for r in rc)
worst = sorted(((rc_by_comm[c] / tot_by_comm[c], rc_by_comm[c], tot_by_comm[c], c) for c in rc_by_comm if tot_by_comm[c] >= 100), reverse=True)
top10 = sum(x[0] for x in sorted(((rc_by_comm[c], c) for c in rc_by_comm), reverse=True)[:10])
print(f"\nround-changes: {len(rc)} ({100*len(rc)/len(dec):.3f}%) | committees ever RC: {len(rc_by_comm)}/{len(tot_by_comm)} "
      f"| worst-10 committees = {100*top10/max(1,len(rc)):.0f}% of all RC")
for r, n, t, c in worst[:5]:
    print(f"  {c[:12]}: {n}/{t} = {100*r:.0f}%")
