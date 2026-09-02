#!/usr/bin/env python3
"""Round-1 QBFT duration across duty types: print the comparison table and render the
log-scale survival chart (assets/round1-duration-by-duty.png).

Reuses the sibling analyses' datasets:
  proposer  duties.jsonl  from ../../proposer-decision-timing/scripts/data   (PROP_DATA)
  attester  attest.jsonl  from ../../attester-decision-timing/scripts/data   (ATT_DATA)
  agg/sync  agg_sync.jsonl produced by collect.py in ./data                   (OUT_DIR)
  drift-removed proposer  drift_removed.json produced by drift.py

Chart rendering shells out to headless Chrome (override with the CHROME env var);
the committed PNG is the output. Table-only if Chrome is absent.

Usage:
    PROP_DATA=../../proposer-decision-timing/scripts/data \
    ATT_DATA=../../attester-decision-timing/scripts/data python compare.py
"""
import os, json, math, bisect, subprocess
HERE = os.path.dirname(__file__)
PROP_DATA = os.environ.get("PROP_DATA", os.path.join(HERE, "..", "..", "proposer-decision-timing", "scripts", "data"))
ATT_DATA = os.environ.get("ATT_DATA", os.path.join(HERE, "..", "..", "attester-decision-timing", "scripts", "data"))
OUT_DIR = os.environ.get("OUT_DIR", os.path.join(HERE, "data"))
CHROME = os.environ.get("CHROME", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")


def pct(a, p):
    a = sorted(a); k = (len(a) - 1) * p / 100; lo, hi = math.floor(k), math.ceil(k)
    return a[int(k)] if lo == hi else a[lo] * (hi - k) + a[hi] * (k - lo)


def durs(rows):
    return sorted(r["decided_ms"] - r["r1_prop_ms"] for r in rows
                  if r.get("decided_round") == 1 and r.get("r1_prop_ms") is not None
                  and r.get("decided_ms") is not None and r["decided_ms"] - r["r1_prop_ms"] >= 0)


P = [json.loads(l) for l in open(f"{PROP_DATA}/duties.jsonl")]
prop = durs([r["qbft"] for r in P if r.get("qbft") and r["qbft"].get("decided_ms") is not None])
att = durs([json.loads(l) for l in open(f"{ATT_DATA}/attest.jsonl")])
AS = [json.loads(l) for l in open(f"{OUT_DIR}/agg_sync.jsonl")]
agg = durs([r for r in AS if r["role"] == "AGGREGATOR"])
sync = durs([r for r in AS if r["role"] == "SYNC_COMMITTEE_CONTRIBUTION"])
lead = sorted(json.load(open(f"{OUT_DIR}/drift_removed.json")))

print(f"{'series':26} {'n':>7} {'p50':>5} {'p90':>5} {'p99':>6} {'p99.9':>6} {'max':>6}")
for name, d in [("proposer observed", prop), ("proposer drift-removed", lead),
                ("aggregator", agg), ("sync-contribution", sync), ("attester", att)]:
    print(f"{name:26} {len(d):>7} {pct(d,50):>5.0f} {pct(d,90):>5.0f} {pct(d,99):>6.0f} {pct(d,99.9):>6.0f} {max(d):>6.0f}")

# ---- log-scale survival chart ----
XMAX, FLOOR = 3600, 0.01
def survival(d, x): return 100 * (len(d) - bisect.bisect_right(d, x)) / len(d)
series = [("attester", att, "var(--crit)", "3293*", 0), ("proposer observed", prop, "var(--r1)", f"{max(prop):.0f}", 0),
          ("proposer drift-removed", lead, "var(--r1)", f"{max(lead):.0f}", 1),
          ("sync-contrib", sync, "var(--sync)", f"{max(sync):.0f}", 0), ("aggregator", agg, "var(--good)", f"{max(agg):.0f}", 0)]
W, H, L, R, T, B = 900, 400, 58, 20, 26, 46
iw, ih, YLO, YHI = W - L - R, H - T - B, -2, 2
sx = lambda x: L + iw * min(x, XMAX) / XMAX
sy = lambda s: T + ih * (1 - (max(math.log10(max(s, FLOOR)), YLO) - YLO) / (YHI - YLO))
svg = [f'<svg viewBox="0 0 {W} {H}">']
for e in (2, 1, 0, -1, -2):
    y = sy(10 ** e); svg.append(f'<line class="grid" x1="{L}" y1="{y:.1f}" x2="{W-R}" y2="{y:.1f}"/>')
    svg.append(f'<text x="{L-8}" y="{y+3:.1f}" text-anchor="end">{ {2:"100%",1:"10%",0:"1%",-1:"0.1%",-2:"0.01%"}[e] }</text>')
for xv in range(0, XMAX + 1, 500):
    svg.append(f'<text x="{sx(xv):.1f}" y="{H-B+18}" text-anchor="middle">{xv/1000:.1f}s</text>')
svg.append(f'<line class="axis" x1="{L}" y1="{T+ih}" x2="{W-R}" y2="{T+ih}"/>')
for name, d, col, mx, dash in series:
    pts = []
    for x in range(0, XMAX + 1, 15):
        s = survival(d, x); pts.append((x, s))
        if s <= 0: break
    path = "M" + " L".join(f"{sx(x):.1f} {sy(s):.2f}" for x, s in pts)
    da = ' stroke-dasharray="7 5"' if dash else ''
    svg.append(f'<path d="{path}" fill="none" stroke="{col}" stroke-width="2.4"{da}/>')
svg.append('</svg>')
leg = " ".join(f'<span><i class="sw" style="{("background:transparent;border:2px dashed "+c) if dash else ("background:"+c)}"></i>{n} (max {m}ms)</span>' for n, _, c, m, dash in series)
FONTS = '<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..48,600;12..48,700&family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap">'
html = f"""<title>chart</title><meta charset="utf-8">{FONTS}
<style>:root{{--ink:#0f151c;--muted:#5c6a7c;--line:#dbe2ea;--grid:#e7edf2;--r1:#0090a3;--good:#1c7a4d;--sync:#b8621b;--crit:#b3182a;}}
*{{box-sizing:border-box}}body{{width:940px;margin:0;background:#fff;font-family:"IBM Plex Sans",sans-serif}}
.wrap{{padding:20px}}h3{{font-family:"Bricolage Grotesque",sans-serif;font-weight:700;font-size:17px;margin:0 0 3px;color:var(--ink);letter-spacing:-.01em}}
.cap{{font-size:12.5px;color:var(--muted);margin:0 0 14px;max-width:92ch;line-height:1.45}}
svg{{width:100%;display:block}}svg text{{font-family:"IBM Plex Mono",monospace;fill:var(--muted);font-size:11px}}
svg .grid{{stroke:var(--grid)}}svg .axis{{stroke:var(--line)}}
.legend{{display:flex;gap:16px;flex-wrap:wrap;margin-top:11px;font-size:12.5px;color:#33404f}}
.legend span{{display:inline-flex;gap:7px;align-items:center}}.sw{{width:11px;height:11px;border-radius:3px;display:inline-block}}</style>
<div class="wrap"><h3>Round-1 QBFT duration tail by duty type &mdash; does the tail match?</h3>
<p class="cap">Share of successful round-1s <b>still not decided</b> X ms after the proposal (log scale; mainnet, 30 sampled epochs). Aggregator and sync-committee-contribution collapse by ~0.7s; only the attester (committee) carries a fat tail to ~3.3s. The proposer's extra tail over aggregator/sync is intra-cluster drift &mdash; removing it (dashed, from per-operator prepare times) drops the proposer's bulk onto the aggregator/sync band. Lower/left = tighter.</p>
{"".join(svg)}
<div class="legend">{leg}</div></div>"""
page = f"{HERE}/_compare.html"; open(page, "w").write(html)
png = os.path.join(HERE, "..", "assets", "round1-duration-by-duty.png")
try:
    subprocess.run([CHROME, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=2",
        "--window-size=940,650", "--virtual-time-budget=4000", "--run-all-compositor-stages-before-draw",
        f"--screenshot={png}", f"file://{page}"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print("rendered", png)
except Exception as e:
    print("chart not rendered (Chrome unavailable?):", e, "-- table above is the reproducible output")
