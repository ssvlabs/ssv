#!/usr/bin/env python3
"""Per-cluster operator QBFT-start drift, from the proposer-duty Exporter traces.

For each cluster, how far apart (in when-into-slot they start QBFT) its fastest and
slowest operator sit. An operator's QBFT start is observed as its round-1 proposal
time into the slot on the duties it leads (the leader rotates through the operators).
Two derivations, both measured from the fastest operator's median start:

  full      : fastest vs the slowest operator.
  practical : fastest vs the slowest operator that still joins the quorum -- QBFT
              needs 2f+1 of n=3f+1, so the f slowest fall out and don't gate
              consensus. Dropped per statistic (typically-slowest for the median,
              worst-on-a-bad-day for the p95), since on any duty it's whoever is
              slowest that misses quorum.

Reliable only where every operator led >=5 duties (rotation) -- the higher-volume
clusters, which handle ~98% of all proposer duties. Reuses the dataset produced by
../../proposer-decision-timing/scripts/collect.py (duties.jsonl + raw/ traces).

Usage:  DATA_DIR=/path/to/proposer-decision-timing/scripts/data python drift.py
Writes drift.json next to this script.
"""
import os, sys, json, gzip, glob, statistics as st
from datetime import datetime
from collections import defaultdict

HERE = os.path.dirname(__file__)
DATA = os.environ.get("DATA_DIR", os.path.join(HERE, "..", "..", "proposer-decision-timing", "scripts", "data"))
GENESIS = 1606824023  # mainnet beacon genesis (unix seconds)
MIN_LEADS = 5         # per-operator lead threshold for a reliable estimate

def ms(iso):
    try:
        return datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() * 1000.0
    except Exception:
        return None

def pctl(v, p):
    v = sorted(v)
    return v[min(len(v) - 1, int(p / 100 * len(v)))]

# committee (operator set) per (slot, validator)
comm = {}
for line in open(os.path.join(DATA, "duties.jsonl")):
    r = json.loads(line)
    comm[(r["slot"], r["val"])] = tuple(r["committee"])

# per cluster: leader -> [proposal-into-slot], and leader -> [proposal - own-RANDAO]
into = defaultdict(lambda: defaultdict(list))
prep = defaultdict(lambda: defaultdict(list))
seen = set()
total_led = 0
for fn in glob.glob(os.path.join(DATA, "raw", "*.json.gz")):
    for t in (json.load(gzip.open(fn)).get("data") or []):
        if t.get("role") != "PROPOSER":
            continue
        try:
            slot, val = int(t["slot"]), int(t["validator"])
        except Exception:
            continue
        c = comm.get((slot, val))
        if not c:
            continue
        seen.add(c)
        proposal = None
        for rd in (t.get("consensus") or []):
            p = rd.get("proposal")
            if isinstance(p, dict) and p.get("round") == 1:
                proposal = p
        if not proposal or proposal.get("leader") is None:
            continue
        pt = ms(proposal.get("time"))
        if pt is None:
            continue
        lead = proposal["leader"]
        into[c][lead].append(pt - (GENESIS + slot * 12) * 1000.0)
        total_led += 1
        pre = {m.get("signer"): ms(m.get("time")) for m in (t.get("pre") or []) if m.get("time")}
        if pre.get(lead) is not None:
            prep[c][lead].append(pt - pre[lead])

out = []
covered = 0
for c, byop in into.items():
    ops = sorted(c)
    n = len(ops)
    if not all(len(byop.get(o, [])) >= MIN_LEADS for o in ops):
        continue
    covered += sum(len(v) for v in byop.values())
    per = {o: {"led": len(byop[o]), "p50": round(st.median(byop[o])), "p95": round(pctl(byop[o], 95)),
               "prep": round(st.median(prep[c].get(o, [0]) or [0]))} for o in ops}
    fastp50 = min(per[o]["p50"] for o in ops)
    for o in ops:
        per[o]["om"] = per[o]["p50"] - fastp50   # median offset from fastest
        per[o]["o95"] = per[o]["p95"] - fastp50   # p95 offset from fastest
    fast = min(ops, key=lambda o: per[o]["p50"])
    f = (n - 1) // 3                              # SSV: n = 3f+1, quorum = 2f+1
    med_desc = sorted(ops, key=lambda o: -per[o]["om"])
    p95_desc = sorted(ops, key=lambda o: -per[o]["o95"])
    out.append({
        "cluster": list(c), "n": sum(len(v) for v in byop.values()), "f": f, "quorum": n - f,
        "fast": fast, "slow": med_desc[0], "siq": med_desc[f],
        "drop_med": med_desc[:f], "drop_p95": p95_desc[:f],
        "g1": per[med_desc[0]]["om"], "g1w": per[p95_desc[0]]["o95"],   # full: worst over all ops
        "g2": per[med_desc[f]]["om"], "g2w": per[p95_desc[f]]["o95"],   # practical: drop f, next worst
        "ops": per,
    })

coverage_pct = round(100 * covered / max(1, total_led), 1)
json.dump({"reliable": len(out), "seen": len(seen), "coverage_pct": coverage_pct, "clusters": out},
          open(os.path.join(HERE, "drift.json"), "w"))
print(f"reliable {len(out)} / {len(seen)} clusters; they cover {coverage_pct}% of proposer duties")
