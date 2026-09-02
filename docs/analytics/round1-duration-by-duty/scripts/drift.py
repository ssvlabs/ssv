#!/usr/bin/env python3
"""Estimate the drift-removed ("zero drift") proposer round-1 duration from the raw
Exporter proposer traces, using per-operator prepare times.

  observed      = decided - proposal                       (gated by the marginal quorum operator)
  drift-removed = observed - (marginal-quorum prepare - fastest non-leader prepare)

The prepare-phase spread (fastest non-leader -> the 2f+1-th prepare) is the quorum's
readiness drift; removing it leaves the intrinsic consensus latency. Writes
drift_removed.json (list of ms) for compare.py, and prints the decomposition.

Uses the raw proposer traces from the proposer analysis (regenerate them with
../../proposer-decision-timing/scripts/collect.py). Point PROP_DATA at that data dir.

Usage:
    PROP_DATA=../../proposer-decision-timing/scripts/data python drift.py
"""
import os, gzip, json, glob, math
from datetime import datetime

PROP_DATA = os.environ.get("PROP_DATA", os.path.join(os.path.dirname(__file__), "..", "..", "proposer-decision-timing", "scripts", "data"))
RAW = os.path.join(PROP_DATA, "raw")
OUT_DIR = os.environ.get("OUT_DIR", os.path.join(os.path.dirname(__file__), "data")); os.makedirs(OUT_DIR, exist_ok=True); OUT = os.path.join(OUT_DIR, "drift_removed.json")
GENESIS, SLOT_DUR = 1606824023, 12


def off_ms(iso, slot):
    try: return (datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() - (GENESIS + slot * SLOT_DUR)) * 1000.0
    except Exception: return None


def pct(a, p):
    a = sorted(a); k = (len(a) - 1) * p / 100; lo, hi = math.floor(k), math.ceil(k)
    return a[int(k)] if lo == hi else a[lo] * (hi - k) + a[hi] * (k - lo)


obs, lead = [], []
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
        marg_prep = pps[min(q - 1, len(pps) - 1)]     # 2f+1-th (marginal quorum) prepare
        drift = max(marg_prep - pps[1], 0)            # spread from fastest non-leader prepare
        obs.append(o); lead.append(max(o - drift, 0))

json.dump(lead, open(OUT, "w"))
print(f"proposer round-1 duties: {len(obs)}  ->  wrote {OUT}")
print(f"{'series':22} {'p50':>5} {'p90':>5} {'p99':>6} {'p99.9':>6} {'max':>6}")
for name, d in [("observed", obs), ("drift-removed", lead)]:
    print(f"{name:22} {pct(d,50):>5.0f} {pct(d,90):>5.0f} {pct(d,99):>6.0f} {pct(d,99.9):>6.0f} {max(d):>6.0f}")
