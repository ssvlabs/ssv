#!/usr/bin/env python3
"""Cross-validate E2M miss/success flags against the canonical beacon chain.

For each duty, query a public mainnet beacon node's header at the duty's slot:
  404 (empty slot)                 -> the proposer missed
  200 with proposer==our validator -> the block landed (canonical, finalized)
Confirms E2M's on-chain outcome flags independently (catches reorg mislabeling).

Usage:  python chain_validate.py [path/to/duties.jsonl]   # validates the misses,
        plus a few late-but-successful controls.
"""
import sys, os, json, time, httpx

DATA = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "data", "duties.jsonl")
BN = os.environ.get("BEACON_API", "https://ethereum-beacon-api.publicnode.com")
rows = [json.loads(l) for l in open(DATA)]

def check(slot):
    try:
        r = httpx.get(f"{BN}/eth/v1/beacon/headers/{slot}", timeout=30)
        if r.status_code == 404:
            return ("EMPTY", None, None)
        r.raise_for_status()
        d = r.json()["data"]
        return ("BLOCK", d["header"]["message"]["proposer_index"], d.get("canonical"))
    except Exception as e:
        return ("ERR:" + type(e).__name__, None, None)

def late_decided(r):  # decided in the 3-4.5s danger zone
    q = r["qbft"]
    return r["has_trace"] and q and q["decided_ms"] is not None and 3000 < q["decided_ms"] < 4500

misses = [r for r in rows if r["onchain"]["success"] is False]
print(f"=== validating {len(misses)} E2M misses (expect EMPTY slots) ===")
bad = 0
for r in sorted(misses, key=lambda r: r["slot"]):
    st, prop, canon = check(r["slot"]); time.sleep(0.6)
    ok = st == "EMPTY"
    bad += 0 if ok else 1
    print(f"  slot {r['slot']} val {r['val']} -> {st:6} {'confirmed miss' if ok else '!! block exists prop=%s' % prop}")
print(f"confirmed {len(misses)-bad}/{len(misses)} misses; {bad} discrepancies")

controls = [r for r in rows if r["onchain"]["success"] is True and late_decided(r)][:4]
print(f"\n=== spot-check {len(controls)} late-but-successful controls (expect BLOCK by same validator) ===")
for r in controls:
    st, prop, canon = check(r["slot"]); time.sleep(0.6)
    ok = st == "BLOCK" and str(prop) == str(r["val"]) and canon
    print(f"  slot {r['slot']} val {r['val']} -> {'confirmed success' if ok else 'CHECK st=%s prop=%s' % (st, prop)}")
