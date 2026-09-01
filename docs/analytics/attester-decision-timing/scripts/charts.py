#!/usr/bin/env python3
"""Regenerate attest_summary.json — the chart data embedded in
../attester-qbft-decision-timing.html.

Usage:  python charts.py [path/to/attest.jsonl]

Rebuild the page by re-injecting the output: replace the `const D={...}` object at
the top of the HTML's <script> with the contents of attest_summary.json.
"""
import sys, os, json, math
from collections import Counter

DATA = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "data", "attest.jsonl")
OUT = os.path.join(os.path.dirname(__file__), "attest_summary.json")
rows = [json.loads(l) for l in open(DATA)]
dec = [r for r in rows if r["decided_ms"] is not None]


def wilson(k, n, z=1.96):
    if n == 0: return (0, 0, 0)
    p = k / n; d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d; h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (100 * p, 100 * max(0, c - h), 100 * min(1, c + h))


def pct(a, p):
    a = sorted(a); k = (len(a) - 1) * p / 100; lo, hi = math.floor(k), math.ceil(k)
    return a[int(k)] if lo == hi else a[lo] * (hi - k) + a[hi] * (k - lo)


# decision-time histogram (250ms bins, 0..7000)
hist = Counter(int(min(r["decided_ms"], 6999) // 250) * 250 for r in dec)
histarr = [[b, hist.get(b, 0)] for b in range(0, 7000, 250)]

# outcome by 500ms decision-time bin (validator-weighted) + Wilson CI on head-vote
fine = []
for lo in range(0, 6500, 500):
    g = [r for r in dec if lo <= r["decided_ms"] < lo + 500]
    nv = sum(r["n_val"] for r in g); ni = sum(r["n_incl"] for r in g)
    nh = sum(r["n_head"] for r in g); no = sum(r["n_opt"] for r in g)
    if nv < 50: continue
    p, l, h = wilson(nh, ni)
    fine.append({"lo": lo, "nv": nv, "incl": round(100 * ni / nv, 3), "head": round(100 * nh / max(1, ni), 3),
                 "head_lo": round(l, 3), "head_hi": round(h, 3), "opt": round(100 * no / max(1, ni), 3)})

# round-1 duration histogram (log-ish bins)
durs = [r["decided_ms"] - r["r1_prop_ms"] for r in dec
        if r["decided_round"] == 1 and r["r1_prop_ms"] is not None and r["decided_ms"] - r["r1_prop_ms"] >= 0]
edges = [0, 20, 40, 60, 100, 150, 250, 400, 700, 1200, 5000]
labels = ["<20", "20-40", "40-60", "60-100", "100-150", "150-250", "250-400", "400-700", "700-1.2k", "1.2k+"]
dh = [0] * (len(edges) - 1)
for d in durs:
    for i in range(len(edges) - 1):
        if edges[i] <= d < edges[i + 1]: dh[i] += 1; break
starts = [r["r1_prop_ms"] for r in dec if r["decided_round"] == 1 and r["r1_prop_ms"] is not None]
r1dur = {"labels": labels, "counts": dh, "pct": {str(p): round(pct(durs, p)) for p in (50, 90, 95, 99)},
         "n": len(durs), "start_pct": {str(p): round(pct(starts, p)) for p in (50, 90, 99)}}

# round-change concentration
tot_by_comm = Counter(r["committee_hash"] for r in dec)
rc_by_comm = Counter(r["committee_hash"] for r in dec if (r["decided_round"] or 1) > 1)
worst = sorted(((rc_by_comm[c] / tot_by_comm[c], rc_by_comm[c], tot_by_comm[c]) for c in rc_by_comm if tot_by_comm[c] >= 100), reverse=True)[:5]
rc_total = sum(rc_by_comm.values())
top10 = sum(x[0] for x in sorted(((rc_by_comm[c], c) for c in rc_by_comm), reverse=True)[:10])
rc = {"rate": round(100 * rc_total / len(dec), 3), "n_comm_rc": len(rc_by_comm), "n_comm": len(tot_by_comm),
      "worst": [{"pct": round(100 * w[0], 1), "rc": w[1], "tot": w[2]} for w in worst],
      "share_top10": round(100 * top10 / max(1, rc_total), 0)}

# round-1 duration tail zoom (700..4050, 150ms bins); artifact = dur>3400 (epoch-boundary)
lo, w = 700, 150; tedges = list(range(lo, 4051, w)); nb = len(tedges) - 1
gen = [0] * nb; art = [0] * nb
for d in (x for x in durs if x > 700):
    i = min(int((d - lo) // w), nb - 1)
    (art if d > 3400 else gen)[i] += 1
tailz = {"edges": tedges, "gen": gen, "art": art, "w": w,
         "n_tail": sum(1 for d in durs if d > 700),
         "genuine_max": round(max(d for d in durs if d <= 3400)),
         "artifact_n": sum(1 for d in durs if d > 3400), "artifact_slots": 3,
         "gt3s_gen": sum(1 for d in durs if 3000 < d <= 3400)}

summ = {"duties": len(rows), "attest": sum(r["n_val"] for r in rows), "incl": sum(r["n_incl"] for r in rows),
        "head": sum(r["n_head"] for r in rows), "opt": sum(r["n_opt"] for r in rows),
        "gt3": sum(1 for r in dec if r["decided_ms"] > 3000), "gt4": sum(1 for r in dec if r["decided_ms"] > 4000),
        "gt5": sum(1 for r in dec if r["decided_ms"] > 5000), "gt6": sum(1 for r in dec if r["decided_ms"] > 6000),
        "ndec": len(dec), "r2": sum(1 for r in dec if r["decided_round"] == 2),
        "hist": histarr, "fine": fine, "epochs": 30, "r1dur": r1dur, "rc": rc, "tailz": tailz}
json.dump(summ, open(OUT, "w"), separators=(",", ":"))
print(f"wrote {OUT}: {summ['attest']:,} attestations, {summ['duties']:,} committee-duties")
