#!/usr/bin/env python3
"""Estimate the drift-removed ("zero drift") proposer round-1 duration from the raw
Exporter proposer traces, using per-operator prepare times.

Round-1 duration decomposes as:
    observed = prepare-quorum wait + commit phase
             = (2f+1-th prepare - proposal) + (decided - 2f+1-th prepare)

The prepare-quorum wait is drift-gated: it is how long the quorum waits for its
marginal operator to become ready, and it captures *both* forms of intra-cluster
drift measured from the leader's proposal:
  - spread among the responding operators, and
  - the "fast leader, slow pack" lag (the leader got its block early and proposed
    while the rest of the cluster was still block-fetching), since a proposer
    non-leader can only prepare once its own fetch completes.

Drift-removal caps that wait at its typical (drift-free) value Q = median wait:
    drift-removed = observed - max(prepare-quorum wait - Q, 0)
Well-synced duties are unchanged; drift duties shed only the excess wait, leaving
the intrinsic ~2-hop consensus latency. Writes drift_removed.json for compare.py.

Uses the raw proposer traces from the proposer analysis (regenerate them with
../../proposer-decision-timing/scripts/collect.py). Point PROP_DATA at that data dir.

Usage:
    PROP_DATA=../../proposer-decision-timing/scripts/data python drift.py
"""
import os, gzip, json, glob, math
from datetime import datetime

PROP_DATA = os.environ.get("PROP_DATA", os.path.join(os.path.dirname(__file__), "..", "..", "proposer-decision-timing", "scripts", "data"))
RAW = os.path.join(PROP_DATA, "raw")
OUT_DIR = os.environ.get("OUT_DIR", os.path.join(os.path.dirname(__file__), "data")); os.makedirs(OUT_DIR, exist_ok=True)
OUT = os.path.join(OUT_DIR, "drift_removed.json")
GENESIS, SLOT_DUR = 1606824023, 12


def off_ms(iso, slot):
    try: return (datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() - (GENESIS + slot * SLOT_DUR)) * 1000.0
    except Exception: return None


def pct(a, p):
    a = sorted(a); k = (len(a) - 1) * p / 100; lo, hi = math.floor(k), math.ceil(k)
    return a[int(k)] if lo == hi else a[lo] * (hi - k) + a[hi] * (k - lo)


obs, qwait, commit = [], [], []
for f in sorted(glob.glob(f"{RAW}/*.json.gz")):
    raw = json.load(gzip.open(f))
    traces = raw if isinstance(raw, list) else (raw.get("data") or raw.get("traces") or [])
    for t in traces:
        if t.get("role") != "PROPOSER": continue
        dec = t.get("decideds") or []; cons = t.get("consensus") or []
        if not dec or dec[0].get("round") != 1: continue
        slot = int(t["slot"])
        r1 = next((rd for rd in cons if (rd.get("proposal") or {}).get("round") == 1), None)
        if not r1 or not r1.get("proposal"): continue
        prop = off_ms(r1["proposal"]["time"], slot)
        pps = sorted(x for x in (off_ms(p.get("time"), slot) for p in (r1.get("prepares") or [])) if x is not None)
        decided = off_ms(dec[0]["time"], slot)
        if prop is None or decided is None or len(pps) < 2: continue
        n = len({c.get("signer") for c in (r1.get("commits") or [])} | {p.get("signer") for p in (r1.get("prepares") or [])})
        q = 2 * ((n - 1) // 3) + 1
        o = decided - prop
        if o < 0: continue
        marg = pps[min(q - 1, len(pps) - 1)]     # 2f+1-th (marginal quorum) prepare
        obs.append(o); qwait.append(max(marg - prop, 0)); commit.append(max(decided - marg, 0))

Q = pct(qwait, 50)                                # typical (drift-free) prepare-quorum wait
lead = [max(obs[i] - max(qwait[i] - Q, 0), 0) for i in range(len(obs))]
json.dump(lead, open(OUT, "w"))
print(f"proposer round-1 duties: {len(obs)}  ->  wrote {OUT}")
print(f"typical prepare-quorum wait Q = {Q:.0f}ms | commit phase median {pct(commit,50):.0f}ms p99.9 {pct(commit,99.9):.0f}ms")
print(f"{'series':22} {'p50':>5} {'p90':>5} {'p99':>6} {'p99.9':>6} {'max':>6}")
for name, d in [("observed", obs), ("drift-removed", lead)]:
    print(f"{name:22} {pct(d,50):>5.0f} {pct(d,90):>5.0f} {pct(d,99):>6.0f} {pct(d,99.9):>6.0f} {max(d):>6.0f}")
print(f"drift-removed >300ms (commit-phase residual): {sum(1 for x in lead if x > 300)}")
