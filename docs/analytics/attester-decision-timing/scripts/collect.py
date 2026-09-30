#!/usr/bin/env python3
"""Collect the attester (committee) duty dataset: Exporter committee traces +
E2M on-chain outcomes (mainnet), stratified one epoch per day.

Reference collector for the analysis in ../README.md. SSV runs one QBFT per
(committee, slot) for attestations; the on-chain outcome is per validator. Per
committee-slot we keep the QBFT decision (round, time) from the Exporter committee
trace and roll up its validators' E2M outcomes.

Each committee-duty row keeps:
  identity : slot, epoch, committee (operator-id set), committee_hash
  qbft     : decided_round, decided_ms, r1_prop_ms, n_rounds, had_round_change
  outcome  : n_val, n_incl, n_head, n_opt   (validators / included / correct-head / optimal-inclusion)

Decision time is the Exporter's receive-time of the aggregated "decided" message
(a commit with >1 signer), relative to slot start. The Exporter committee-traces
endpoint returns every committee for a slot range with no CommitteeIDs, so one
request per epoch covers the whole network. E2M `/api/duties` returns all attester
duties for a one-epoch range in a single response.

Attester duties are ~1000x proposer volume, so the window is *sampled* (one epoch
per day) rather than fully enumerated — mirroring the committee sampling in
ssvlabs/ssv#2883.

Dependencies (internal): run inside an ssv-scout workspace
(github.com/ssvlabs/ssv-scout) with an active Teleport vnet tunnel — it provides
the Exporter / E2M endpoints via `config.ENVIRONMENTS`. Point SSV_SCOUT_DIR at it.

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
SPE, EPD = 32, 225                                  # slots/epoch, epochs/day (~)
DAYS = max(1, (END_EPOCH - START_EPOCH) // EPD)
random.seed(42)                                     # reproducible stratified sample
EPOCHS = sorted(START_EPOCH + d * EPD + random.randint(0, EPD - 1) for d in range(DAYS))


def off_ms(iso, slot):
    if not iso:
        return None
    try:
        return (datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() - (GENESIS + slot * SLOT_DUR)) * 1000.0
    except Exception:
        return None


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


def main():
    t0 = time.time()
    print(f"sampled epochs ({len(EPOCHS)}): {EPOCHS[0]}..{EPOCHS[-1]}", flush=True)
    fout = open(f"{OUT}/attest.jsonl", "w")
    n_duties = 0
    for ei, ep in enumerate(EPOCHS):
        fr, to = ep * SPE, ep * SPE + SPE - 1
        # E2M attester outcomes for the epoch: (slot, validator) -> (inclusion_slot, correct_head)
        e = get(f"{E2M}/api/duties", {"from": ep, "to": ep, "types": "attest"})
        outc = {}
        for a in (e.get("Duties") or {}).get("Attesters") or []:
            try:
                slot, val = int(a["Slot"]), int(a["ValidatorIndex"])
            except Exception:
                continue
            try:
                incl = int(a.get("InclusionSlot") or 0)
            except Exception:
                incl = 0
            outc[(slot, val)] = (incl, bool(a.get("CorrectHeadVote")))
        # Exporter committee traces for the epoch (all committees, no IDs)
        traces = post(f"{EXP}/v1/exporter/traces/committee", {"From": fr, "To": to}).get("data") or []
        for t in traces:
            try:
                slot = int(t["slot"])
            except Exception:
                continue
            dec = t.get("decideds") or []
            cons = t.get("consensus") or []
            att = t.get("attester") or []
            if not att:                              # committee-slot with no attestation duty (sync only)
                continue
            vals = sorted({v for s in att for v in (s.get("validatorIdx") or [])})
            ops = sorted({s.get("signer") for s in att if s.get("signer") is not None})
            r1 = None
            for rd in cons:
                p = rd.get("proposal")
                if isinstance(p, dict) and p.get("round") == 1:
                    r1 = off_ms(p.get("time"), slot)
            n_val = n_incl = n_head = n_opt = 0
            for v in vals:
                o = outc.get((slot, v))
                if o is None:
                    continue
                n_val += 1
                incl, head = o
                if incl > 0:
                    n_incl += 1
                    if head: n_head += 1
                    if incl - slot == 1: n_opt += 1
            fout.write(json.dumps({
                "slot": slot, "epoch": ep, "committee": ops, "committee_hash": t.get("committeeID"),
                "decided_round": dec[0].get("round") if dec else None,
                "decided_ms": off_ms(dec[0].get("time"), slot) if dec else None,
                "r1_prop_ms": r1, "n_rounds": len(cons),
                "had_round_change": any((rd.get("roundChanges") or []) for rd in cons),
                "n_val": n_val, "n_incl": n_incl, "n_head": n_head, "n_opt": n_opt,
            }) + "\n")
            n_duties += 1
        print(f"  epoch {ep} ({ei+1}/{len(EPOCHS)}): {len(traces)} committee-slots, cum duties {n_duties}, {time.time()-t0:.0f}s", flush=True)
    fout.close()
    print(f"wrote {OUT}/attest.jsonl: {n_duties} committee-duties in {time.time()-t0:.0f}s", flush=True)


if __name__ == "__main__":
    main()
