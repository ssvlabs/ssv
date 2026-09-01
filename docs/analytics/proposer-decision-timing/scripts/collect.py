#!/usr/bin/env python3
"""Collect the proposer duty dataset: e2m spine + Exporter QBFT traces (mainnet).

Reference collector used to produce the analysis in ../README.md. Each duty keeps
three separated layers:
  identity : slot, epoch, validator, committee (operator-id set)
  qbft     : decided_round, decided_ms, r1_prop_ms, first_pre_ms, n_rounds, had_round_change
  onchain  : success, mev_reward, block_number   (overlay only)

Decision time is Exporter commit-quorum receive-time relative to slot start.
Adaptive batching bisects on the Exporter's ~20s per-request compute ceiling.

Dependencies (internal): run inside an ssv-scout workspace
(github.com/ssvlabs/ssv-scout) with an active Teleport vnet tunnel — it provides
the Exporter / E2M endpoints via `config.ENVIRONMENTS`. Point SSV_SCOUT_DIR at it.

Usage:
    SSV_SCOUT_DIR=/path/to/ssv-scout OUT_DIR=./data python collect.py <start_epoch> <end_epoch>
"""
import sys, os, json, time, gzip, threading
from datetime import datetime
from concurrent.futures import ThreadPoolExecutor, as_completed
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
os.makedirs(f"{OUT}/raw", exist_ok=True)

START_EPOCH = int(sys.argv[1]) if len(sys.argv) > 1 else 465250
END_EPOCH = int(sys.argv[2]) if len(sys.argv) > 2 else 472000
EPD, SPE, W_SLOTS, N_MAX, WORKERS = 225, 32, 400, 80, 3

def slot_start(s): return GENESIS + s * SLOT_DUR
def off_ms(iso, s):
    if not iso:
        return None
    try:
        t = datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp()
    except Exception:
        return None
    return (t - slot_start(s)) * 1000.0

_lock = threading.Lock()
def log(*a):
    with _lock:
        print(*a, flush=True)

def e2m_get(params, tries=4):
    for i in range(tries):
        try:
            r = httpx.get(f"{E2M}/api/duties", params=params, timeout=120)
            r.raise_for_status()
            return r.json()
        except Exception:
            if i == tries - 1:
                raise
            time.sleep(2 * (i + 1))

def exp_post(from_s, to_s, idx, tries=3):
    body = {"From": from_s, "To": to_s, "Indices": idx, "Roles": ["PROPOSER"]}
    for i in range(tries):
        try:
            r = httpx.post(f"{EXP}/v1/exporter/traces/validator", json=body, timeout=60)
            r.raise_for_status()
            return r.json()
        except httpx.HTTPStatusError:
            raise
        except Exception:
            if i == tries - 1:
                raise
            time.sleep(1.5 * (i + 1))

def collect_spine():
    spine, ef = {}, START_EPOCH
    while ef <= END_EPOCH:
        et = min(ef + 7 * EPD - 1, END_EPOCH)  # 7-day chunks stay under e2m's propose-only cap
        data = e2m_get({"from": ef, "to": et, "types": "propose", "with_committee": ""})
        for d in (data.get("Duties") or {}).get("Proposers") or []:
            try:
                slot, val = int(d["Slot"]), int(d["ValidatorIndex"])
            except Exception:
                continue
            spine[(slot, val)] = {
                "slot": slot, "epoch": slot // SPE, "val": val,
                "committee": sorted(d.get("Committee") or []), "success": bool(d.get("Success")),
                "mev_reward": d.get("MEVReward"), "block_number": d.get("BlockNumber"),
            }
        log(f"  e2m {ef}-{et}: cum {len(spine)}")
        ef = et + 1
    return spine

def extract(t):
    try:
        slot, val = int(t.get("slot")), int(t.get("validator"))
    except Exception:
        return None
    per_round = {}
    for rd in t.get("consensus") or []:
        p = rd.get("proposal")
        if isinstance(p, dict) and p.get("round") is not None:
            per_round[p["round"]] = off_ms(p.get("time"), slot)
    dec = t.get("decideds") or []
    pre = [off_ms(m.get("time"), slot) for m in (t.get("pre") or []) if m.get("time")]
    pre = [x for x in pre if x is not None]
    return {
        "slot": slot, "val": val,
        "decided_round": dec[0].get("round") if dec else None,
        "decided_ms": off_ms(dec[0].get("time"), slot) if dec else None,
        "n_decided_signers": len(dec[0].get("signers") or []) if dec else 0,
        "r1_prop_ms": per_round.get(1), "n_rounds": len(t.get("consensus") or []),
        "had_round_change": any((rd.get("roundChanges") or []) for rd in t.get("consensus") or []),
        "first_pre_ms": min(pre) if pre else None,
        "prop_offsets_by_round": per_round, "committee_hash": t.get("committeeID"),
    }

def fetch_batch(pairs, spine, depth=0):
    slots = [s for s, _ in pairs]
    idx = sorted({v for _, v in pairs})
    try:
        resp = exp_post(min(slots), max(slots), idx)
        try:
            with gzip.open(f"{OUT}/raw/resp_{min(slots)}_{max(slots)}.json.gz", "wt") as f:
                json.dump(resp, f)
        except Exception:
            pass
        rows = []
        for t in resp.get("data") or []:
            r = extract(t)
            if r and (r["slot"], r["val"]) in spine:
                rows.append(r)
        return rows
    except Exception as e:
        if len(pairs) > 1 and depth < 12:  # bisect around the Exporter's per-request ceiling
            mid = len(pairs) // 2
            return fetch_batch(pairs[:mid], spine, depth + 1) + fetch_batch(pairs[mid:], spine, depth + 1)
        log(f"    DROP {len(pairs)} duty(s) near slot {min(slots)}: {type(e).__name__}")
        return []

def make_windows(spine):
    pairs = sorted(spine.keys())
    windows, cur = [], []
    for p in pairs:
        if cur and (p[0] - cur[0][0] > W_SLOTS or len(cur) >= N_MAX):
            windows.append(cur)
            cur = []
        cur.append(p)
    if cur:
        windows.append(cur)
    return windows

def main():
    t0 = time.time()
    log(f"window epochs {START_EPOCH}-{END_EPOCH}  W={W_SLOTS} N={N_MAX} workers={WORKERS}")
    spine = collect_spine()
    log(f"spine: {len(spine)} proposer duties")
    windows = make_windows(spine)
    joined, done = {}, 0
    with ThreadPoolExecutor(max_workers=WORKERS) as ex:
        futs = {ex.submit(fetch_batch, w, spine): w for w in windows}
        for fut in as_completed(futs):
            for r in fut.result():
                joined[(r["slot"], r["val"])] = r
            done += 1
            if done % 25 == 0 or done == len(windows):
                log(f"  {done}/{len(windows)} windows, {len(joined)} traces, {time.time()-t0:.0f}s")
    log(f"trace coverage: {len(joined)}/{len(spine)} = {100*len(joined)/max(1,len(spine)):.2f}%")
    with open(f"{OUT}/duties.jsonl", "w") as f:
        for key, sp in spine.items():
            q = joined.get(key)
            f.write(json.dumps({
                "slot": sp["slot"], "epoch": sp["epoch"], "val": sp["val"], "committee": sp["committee"],
                "onchain": {"success": sp["success"], "mev_reward": sp["mev_reward"], "block_number": sp["block_number"]},
                "qbft": None if not q else {k: q[k] for k in (
                    "decided_round", "decided_ms", "r1_prop_ms", "first_pre_ms", "n_rounds",
                    "had_round_change", "n_decided_signers", "prop_offsets_by_round", "committee_hash")},
                "has_trace": q is not None,
            }) + "\n")
    log(f"wrote {OUT}/duties.jsonl in {time.time()-t0:.0f}s")

if __name__ == "__main__":
    main()
