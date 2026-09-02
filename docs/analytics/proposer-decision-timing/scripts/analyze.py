#!/usr/bin/env python3
"""Reproduce the numbers in ../README.md from the collected dataset (duties.jsonl).

Usage:  python analyze.py [path/to/duties.jsonl]

Covers: decision-time distribution & thresholds, round x time, miss-rate by round,
per-cluster ProposerDelay estimate (SSV-Labs-calibrated) and bucketing, forward vs
backward bucket rates, late-decide concentration tables (>2500ms / >3000ms), and the
round-1 QBFT consensus-duration distribution.
"""
import sys, os, json, math, statistics as st
from collections import defaultdict, Counter

DATA = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "data", "duties.jsonl")
rows = [json.loads(l) for l in open(DATA)]
tr = [r for r in rows if r["has_trace"] and r["qbft"]]
dec = [r for r in tr if r["qbft"]["decided_ms"] is not None]

# --- SSV Labs is committee {1,2,3,4}, runs ProposerDelay=0 -> its median r1 proposal is the fetch baseline
BASE = 1115  # SSV-Labs-measured median round-1 proposal offset (ms); see README methodology
cl = defaultdict(list)
for r in tr:
    if r["qbft"]["r1_prop_ms"] is not None:
        cl[tuple(r["committee"])].append(r["qbft"]["r1_prop_ms"])
meds = {c: st.median(v) for c, v in cl.items() if len(v) >= 8}  # reliable clusters only

def bucket(c):
    # Coarse SSV-Labs-calibrated tiers: block-fetch varies ~375ms across clusters,
    # wider than a 400ms bucket, so only the >=700ms delay tier is separable.
    m = meds.get(c)
    if m is None:
        return "unknown(n<8)"
    d = max(0, m - BASE)
    return "<=700" if d < 700 else ("700-1000" if d < 1000 else "1000+")

BUCKETS = ("<=700", "700-1000", "1000+", "unknown(n<8)")

def wilson(k, n, z=1.96):
    if n == 0:
        return (0.0, 0.0, 0.0)
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (100 * p, 100 * max(0, c - h), 100 * min(1, c + h))

def rk(x):
    return "none" if x is None else ("3+" if x >= 3 else str(x))

print(f"duties={len(rows)}  traced={len(tr)} ({100*len(tr)/len(rows):.2f}%)  decided={len(dec)}")
print(f"on-chain misses: {sum(1 for r in rows if r['onchain']['success'] is False)}\n")

# --- decision-time distribution & thresholds
dms = sorted(r["qbft"]["decided_ms"] for r in dec)
def pctile(a, p):
    k = (len(a) - 1) * p / 100
    lo, hi = math.floor(k), math.ceil(k)
    return a[int(k)] if lo == hi else a[lo] * (hi - k) + a[hi] * (k - lo)
print("decision-ms percentiles:", {p: round(pctile(dms, p)) for p in (50, 90, 95, 99)})
for th in (2000, 2500, 3000, 3500, 4000):
    k = sum(1 for x in dms if x > th)
    print(f"  decide > {th}ms: {k} ({100*k/len(dms):.3f}%)")

# --- round x time + miss-by-round
print("\nround x time (decided):")
for lo, hi, lbl in [(0, 2000, "<2s"), (2000, 3000, "2-3s"), (3000, 3500, "3-3.5s"), (3500, 4000, "3.5-4s"), (4000, 1e12, ">4s")]:
    g = [r for r in dec if lo <= r["qbft"]["decided_ms"] < hi]
    print(f"  {lbl:8}: r1={sum(1 for r in g if r['qbft']['decided_round']==1)} r2={sum(1 for r in g if r['qbft']['decided_round']==2)}")
print("miss-rate by round:")
for k in ("1", "2", "3+"):
    g = [r for r in tr if rk(r["qbft"]["decided_round"]) == k]
    m = sum(1 for r in g if r["onchain"]["success"] is False)
    print(f"  round {k:2}: {m}/{len(g)} = {wilson(m, len(g))[0]:.3f}%")
noc = [r for r in tr if r["qbft"]["decided_round"] is None]  # traced but never decided
print(f"  no-consensus: {sum(1 for r in noc if r['onchain']['success'] is False)}/{len(noc)}")

# --- ProposerDelay buckets. Forward rate P(late|bucket) is the causal read;
#     backward share P(bucket|late) is base-rate biased (see README).
print(f"\nProposerDelay buckets (baseline {BASE}ms) -- forward P(outcome|bucket):")
for b in BUCKETS:
    g = [r for r in dec if bucket(tuple(r["committee"])) == b]
    if not g:
        print(f"  {b:14}: 0 duties")
        continue
    late = sum(1 for r in g if r["qbft"]["decided_ms"] > 3000)
    rc = sum(1 for r in g if r["qbft"]["had_round_change"])
    miss = sum(1 for r in g if r["onchain"]["success"] is False)
    print(f"  {b:14}: {len(g):6} duties | >3s {wilson(late,len(g))[0]:.3f}% | round-change {100*rc/len(g):.3f}% | miss {miss}")
late_all = [r for r in dec if r["qbft"]["decided_ms"] > 3000]
print("  backward P(bucket|>3s) vs base rate:")
for b in BUCKETS:
    share = sum(1 for r in late_all if bucket(tuple(r["committee"])) == b)
    base = sum(1 for r in dec if bucket(tuple(r["committee"])) == b)
    print(f"    {b:14}: {100*share/len(late_all):4.1f}% of late   vs base {100*base/len(dec):4.1f}%")

# --- late-decide concentration (>2500 and >3000)
total_by = Counter(tuple(r["committee"]) for r in tr)
for TH in (3000, 2500):
    g = [r for r in dec if r["qbft"]["decided_ms"] > TH]
    late = Counter(tuple(r["committee"]) for r in g)
    red = Counter(tuple(r["committee"]) for r in g if r["onchain"]["success"] is False)
    r2 = sum(1 for r in g if r["qbft"]["decided_round"] == 2)
    print(f"\n>{TH}ms: {len(g)} duties (r2={r2}, miss={sum(red.values())}) across {len(late)} clusters")
    for c, n in late.most_common():
        if n < 2:
            continue
        print(f"    {str(list(c)):32} late={n} miss={red.get(c,0)} total={total_by[c]} rate={100*n/total_by[c]:.1f}%")

# --- round-1 QBFT consensus duration (decided - proposal), round-1 only
durs = sorted(r["qbft"]["decided_ms"] - r["qbft"]["r1_prop_ms"]
              for r in tr if r["qbft"]["decided_round"] == 1
              and r["qbft"]["decided_ms"] is not None and r["qbft"]["r1_prop_ms"] is not None
              and r["qbft"]["decided_ms"] - r["qbft"]["r1_prop_ms"] >= 0)
print(f"\nround-1 duration (n={len(durs)}): median={pctile(durs,50):.0f} p99={pctile(durs,99):.0f} max={durs[-1]:.0f}")
for th in (100, 500, 1000):
    print(f"  > {th}ms: {sum(1 for x in durs if x > th)} ({100*sum(1 for x in durs if x>th)/len(durs):.3f}%)")
