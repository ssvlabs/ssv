#!/usr/bin/env python3
"""Aggregator + sync-committee-contribution round-1 timing sample (mainnet).

Per-validator duties (like the proposer): fetch Exporter validator traces filtered
by role, keep only consensus-present (selected) duties, and record round-1 timing.
Aggregators are sampled across the validator population; sync-contribution is
targeted at each epoch's sync-committee validators (from E2M).

The proposer and attester round-1 durations this is compared against come from the
sibling analyses' datasets (../../proposer-decision-timing, ../../attester-decision-timing).

Usage:
    SSV_SCOUT_DIR=/path/to/ssv-scout OUT_DIR=./data python collect.py <start_epoch> <end_epoch>
"""
import sys, os, json, time, random
from datetime import datetime
import httpx

SSV_SCOUT_DIR = os.environ.get("SSV_SCOUT_DIR")
if not SSV_SCOUT_DIR:
    sys.exit("set SSV_SCOUT_DIR to your ssv-scout checkout (provides Exporter/E2M config)")
sys.path.insert(0, SSV_SCOUT_DIR)
from config import ENVIRONMENTS  # noqa: E402

ENV = ENVIRONMENTS["prod-mainnet"]
GENESIS, SLOT_DUR = ENV.beacon_genesis_time_sec, ENV.slot_duration_sec
E2M, EXP = ENV.e2m_api_url.rstrip("/"), ENV.ssv_node_api_url.rstrip("/")
OUT = os.environ.get("OUT_DIR", os.path.join(os.path.dirname(__file__), "data"))
os.makedirs(OUT, exist_ok=True)

START_EPOCH = int(sys.argv[1]) if len(sys.argv) > 1 else 465250
END_EPOCH = int(sys.argv[2]) if len(sys.argv) > 2 else 472000
SPE, EPD, AGG_SAMPLE = 32, 225, 2500
DAYS = max(1, (END_EPOCH - START_EPOCH) // EPD)
random.seed(42)
EPOCHS = sorted(START_EPOCH + d * EPD + random.randint(0, EPD - 1) for d in range(DAYS))


def off_ms(iso, slot):
    try: return (datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() - (GENESIS + slot * SLOT_DUR)) * 1000.0
    except Exception: return None


def get(url, params, tries=4):
    for i in range(tries):
        try:
            r = httpx.get(url, params=params, timeout=180); r.raise_for_status(); return r.json()
        except Exception:
            if i == tries - 1: raise
            time.sleep(2 * (i + 1))


def post(url, body, tries=4):
    for i in range(tries):
        try:
            r = httpx.post(url, json=body, timeout=180); r.raise_for_status(); return r.json()
        except Exception:
            if i == tries - 1: raise
            time.sleep(2 * (i + 1))


def extract(t):
    slot = int(t["slot"]); cons = t.get("consensus") or []; dec = t.get("decideds") or []
    if not cons or not dec: return None
    r1 = None
    for rd in cons:
        p = rd.get("proposal")
        if isinstance(p, dict) and p.get("round") == 1: r1 = off_ms(p.get("time"), slot)
    return {"role": t.get("role"), "slot": slot, "validator": int(t.get("validator") or 0),
            "decided_round": dec[0].get("round"), "decided_ms": off_ms(dec[0].get("time"), slot),
            "r1_prop_ms": r1, "n_rounds": len(cons)}


def main():
    t0 = time.time(); fout = open(f"{OUT}/agg_sync.jsonl", "w"); n_agg = n_sync = 0
    for ei, ep in enumerate(EPOCHS):
        fr, to = ep * SPE, ep * SPE + SPE - 1
        e = get(f"{E2M}/api/duties", {"from": ep, "to": ep, "types": "attest"})
        vidx = sorted({int(a["ValidatorIndex"]) for a in (e.get("Duties") or {}).get("Attesters") or []})
        stride = max(1, len(vidx) // AGG_SAMPLE); sample = vidx[::stride][:AGG_SAMPLE]
        data = post(f"{EXP}/v1/exporter/traces/validator", {"From": fr, "To": to, "Indices": sample, "Roles": ["AGGREGATOR"]}).get("data") or []
        for t in data:
            rec = extract(t)
            if rec: fout.write(json.dumps(rec) + "\n"); n_agg += 1
        s = get(f"{E2M}/api/duties", {"from": ep, "to": ep, "types": "sync_committee"})
        sidx = sorted({int(x["ValidatorIndex"]) for x in (s.get("Duties") or {}).get("SyncCommittee") or []})
        if sidx:
            sd = post(f"{EXP}/v1/exporter/traces/validator", {"From": fr, "To": to, "Indices": sidx, "Roles": ["SYNC_COMMITTEE_CONTRIBUTION"]}).get("data") or []
            for t in sd:
                rec = extract(t)
                if rec: fout.write(json.dumps(rec) + "\n"); n_sync += 1
        print(f"  epoch {ep} ({ei+1}/{len(EPOCHS)}): agg {n_agg}, sync {n_sync}, {time.time()-t0:.0f}s", flush=True)
    fout.close()
    print(f"wrote {OUT}/agg_sync.jsonl: {n_agg} aggregator + {n_sync} sync-contribution duties", flush=True)


if __name__ == "__main__":
    main()
