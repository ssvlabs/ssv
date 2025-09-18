# SSV Exporter v2 — Pre‑Release Notes (v2.0.0‑rc.1)

> **Audience:** SSV node operators and clients integrating with the exporter API.  
> **Focus:** configuration (files/flags/env), API surface (HTTP + WebSocket), compatibility, and upgrade steps.  
> **Compatibility:** v2 keeps legacy clients working. 
>     - In *standard* mode, the legacy `/v1/exporter/decideds` response keeps the v1 format and error semantics (fail‑fast on processing errors). Error message strings may differ from exporter v1. 
>     - In *archive* mode, the same path is backed by the new v2 pipeline and returns a retro‑compatible `data` entry plus an additional `errors` array; processing is **best‑effort** and returns all valid results when possible. If a request yields **no valid results** and **at least one meaningful error**, the API returns an HTTP error.

---

## Overview

Exporter v2 introduces **full duty tracing** (consensus rounds, pre/post partial signatures), **committee‑level views**, richer filtering, OpenAPI definitions, and a clearer configuration model with **two operating modes**:

- **archive** — full traces persisted to disk, new `/traces/*` endpoints, and `/decideds` served by the v2 pipeline (adds `errors` array).
- **standard** — v1‑compatible participants only; the legacy `/decideds` payload is preserved and old data is pruned automatically.

v2 uses **best‑effort responses**: it returns partial results alongside non‑fatal issues in an `errors` array. If a request yields **no valid data** and **at least one meaningful error**, it returns an HTTP error instead of an empty success.

---

## Quick Start

### 1) Choose a mode and enable the exporter

```yaml
# config.yaml (excerpt)
exporter:
  Enabled: true              # enable exporter pipeline
  Mode: archive              # "archive" | "standard"
  RetainSlots: 50400         # used in standard mode only (~7 days at 12s/slot)
```

**Archive mode requirements & effects**
- Persists detailed traces on disk (Pebble). Higher CPU/disk and P2P traffic (subscribes to all subnets).
- Enables new endpoints under `/v1/exporter/traces/*` and serves `/v1/exporter/decideds` via the v2 handler (adds `errors`).

**Standard mode**
- No duty tracer or trace persistence. Keeps legacy participants flow and response shape.
- Background pruning based on `RetainSlots` (initial prune and then continuous, slot‑aligned).
 - When the exporter is enabled, the node subscribes to all subnets unless you configure fixed subnets.

### 2) Expose API ports

```yaml
# HTTP API (required for clients)
SSVAPIPort: 16000

# WebSocket API (optional; use a different port than SSVAPIPort)
WebSocketAPIPort: 16001
WithPing: true
```

> The OpenAPI spec is versioned under `docs/api/ssvnode.openapi.yaml|json`.

### 3) Minimal working configs

**Archive mode (recommended for full tracing)**

```yaml
global:
  LogLevel: info

db:
  Path: ./data/db

eth2:
  BeaconNodeAddr: http://your-beacon:5052

eth1:
  ETH1Addr: ws://your-execution:8546/ws

# p2p: optional fixed subnets. When the exporter is enabled (any mode),
#      the node subscribes to all subnets to capture duties unless you set fixed subnets.

exporter:
  Enabled: true
  Mode: archive

SSVAPIPort: 16000
WebSocketAPIPort: 16001
WithPing: true
```

**Standard mode (v1‑compatible)**

```yaml
exporter:
  Enabled: true
  Mode: standard
  RetainSlots: 50400

SSVAPIPort: 16000
# WebSocket is optional; /stream works in standard mode for decided notifications.
WebSocketAPIPort: 16001
```

---

## Configuration & Flags

- **New top‑level block:** `exporter.Enabled`, `exporter.Mode`, `exporter.RetainSlots`.
- **Deprecated:** `ssv.ValidatorOptions.Exporter: true` (replace with the block above). Any previous example configs using that key must be updated.
- **Storage backend:** When the exporter is enabled, the node uses **Pebble** automatically for exporter data. Standard mode still uses Pebble but does **not** persist full duty traces.
- **Rate limiting:** Endpoints may return **`429 Too Many Requests`**. Clients should back off and retry with jitter.

> If your deployment uses environment variable overrides for YAML keys, use your usual `FOO__BAR` convention (no change introduced by v2).

---

## HTTP API (v2)

All endpoints accept filters in the **query string** or as a **JSON body** (same request struct). Field constraints are summarized in the Appendix.

### `GET|POST /v1/exporter/traces/validator`  _(archive mode only)_

Detailed per‑validator duty traces: consensus rounds (proposal, prepares, commits, round‑changes), decided messages, and pre/post partial signatures. Optional proposal data is returned when available.

**Request fields** (`ValidatorTracesRequest`)
- `from`, `to` — inclusive slot range (int64 ≥ 0).
- `roles` — one or more of `ATTESTER`, `AGGREGATOR`, `PROPOSER`, `SYNC_COMMITTEE`, `SYNC_COMMITTEE_CONTRIBUTION`.
- `pubkeys` — array of 96‑hex BLS pubkeys.
- `indices` — array of validator indices (int64 ≥ 0).

**Response** (`ValidatorTracesResponse`)
- `data: ValidatorTrace[]` — items include `slot`, `role`, `validator`, optional `committeeID`, `consensus` (rounds), `decideds`, `pre`/`post` partial signature traces, and `proposalData` when available.
- `errors?: string[]` — aggregated non‑fatal issues observed while processing.

**Examples**
```bash
curl -s "http://localhost:16000/v1/exporter/traces/validator?from=123456&to=123460&roles=ATTESTER,AGGREGATOR&indices=123,456"

curl -s -X POST http://localhost:16000/v1/exporter/traces/validator   -H 'Content-Type: application/json'   -d '{"from":123456,"to":123460,"roles":["ATTESTER"],"pubkeys":["<96-hex>"],"indices":[123]}'
```

---

### `GET|POST /v1/exporter/traces/committee`  _(archive mode only)_

Committee‑level traces: consensus rounds plus post‑consensus signer data for attester and sync‑committee duties, grouped by committee.

**Request fields** (`CommitteeTracesRequest`)
- `from`, `to` — inclusive slot range (int64 ≥ 0).
- `committeeIDs` — optional list of 64‑hex IDs (32‑byte). If omitted, returns all committees for the specified slots.

**Response** (`CommitteeTracesResponse`)
- `data: CommitteeTrace[]` — items include `slot`, `committeeID`, `proposalData` (when available), `consensus` (rounds), `decideds`, and per‑validator signer data.
- `errors?: string[]`.

**Examples**
```bash
curl -s "http://localhost:16000/v1/exporter/traces/committee?from=123456&to=123460&committeeIDs=<64-hex>"

curl -s -X POST http://localhost:16000/v1/exporter/traces/committee   -H 'Content-Type: application/json'   -d '{"from":123456,"to":123460,"committeeIDs":["<64-hex>"]}'
```

---

### `GET|POST /v1/exporter/decideds`  _(both modes)_

- **Standard mode:** returns the *v1 payload*: `{ data: ParticipantResponse[] }`.  
- **Archive mode:** same path and returns `{ data: DecidedParticipant[], errors?: string[] }`. The `errors` field is **additive** and does not remove existing fields (`role`, `slot`, `public_key`, `message.Signers`).

**Filters** (`DecidedsRequest`)
- `from`, `to` — inclusive slot range (int64 ≥ 0).
- `roles` — same enum as above.
- `pubkeys` — 96‑hex pubkeys.
- `indices` — validator indices (int64 ≥ 0).

**Behavior (best‑effort)**  
Expected “not found” conditions (e.g., no duty for a slot) are **not** treated as API errors. Non‑fatal processing issues are aggregated into `errors`. If **no valid results** are found **and** at least one meaningful error exists, the API returns an error; otherwise it returns partial data plus `errors`.

Note: In standard mode, the `indices` filter is ignored for this endpoint; use `pubkeys` to filter. In archive mode, both `pubkeys` and `indices` are supported.

**Examples**
```bash
# Standard mode → v1 payload
curl -s "http://localhost:16000/v1/exporter/decideds?from=123456&to=123460&roles=ATTESTER&pubkeys=<96-hex>"

# Archive mode → richer payload with `errors`
curl -s "http://localhost:16000/v1/exporter/decideds?from=123456&to=123460&roles=ATTESTER"
```

---

## WebSocket API

When `WebSocketAPIPort` is configured and the exporter is enabled:

- `ws://<host>:<port>/stream` — **decided (quorum) events** published once per `(validator, role, root)` with a **1‑minute de‑duplication window**.  
  Where possible, listeners backfill pubkeys from the registry for retro‑compatibility.
- `ws://<host>:<port>/query` — retained for ad‑hoc lookups (internal format), unchanged from v1.

---

## Compatibility & Behavior Changes

- **Retro‑compatibility:** The legacy `/v1/exporter/decideds` remains available in both modes. In archive mode it is powered by the new backend and **adds** `errors` to the response (existing fields preserved). Clients that strictly validate schemas should allow unknown fields.
- **Two modes:** Archive (full traces, Pebble, all subnets) vs. Standard (participants only, pruning).
- **Best‑effort processing:** Partial results with `errors` instead of hard failures where possible; empty‑with‑errors yields HTTP error.
- **Rate limiting:** APIs may respond with `429 Too Many Requests`; clients must implement backoff with jitter.
- **OpenAPI:** Specs are versioned under `docs/api/ssvnode.openapi.yaml|json`.

---

## Operational Notes

- **Performance & storage (archive):** Persisting detailed traces increases CPU, memory, disk, and P2P usage. Traces are evicted from memory after a few slots and merged from disk on late arrival.
- **Pruning (standard):** Initial prune to `currentSlot - RetainSlots` followed by continuous per‑slot pruning.
- **Health checks:** After enabling v2, confirm `GET /v1/node/health` is healthy, then call a small `/traces/validator` range (archive) or `/decideds` (standard) to validate visibility.

---

## Upgrade Guide

1. **Upgrade** your node to a build that includes Exporter v2.
2. **Migrate config:**
   - Remove `ssv.ValidatorOptions.Exporter: true` if present.
   - Add the new `exporter` block with `Enabled` and `Mode` (and `RetainSlots` for standard).
3. **Expose API(s):** set `SSVAPIPort` (HTTP); optionally set `WebSocketAPIPort` (streaming).
4. **Restart** the node. With archive mode enabled, the exporter data store switches to **Pebble** automatically.
5. **Validate**: probe `/v1/exporter/decideds` and (if archive) `/v1/exporter/traces/validator`; connect to `/stream` to observe live decided events.

---

## Troubleshooting

### Using the logs

- Enable DEBUG logs for the first run of exporter v2 to see migration and GC progress.
- Database migrations can take time. Before upgrading, back up your database. Look for this INFO log to confirm completion:
    - migration completed
- Post‑migration garbage collection (GC) can take even longer. Look for this DEBUG log to confirm GC completion:
    - post-migrations garbage collection completed
      Until GC completes, the HTTP API and exporter components do not start. Note: the consensus client initializes earlier and may emit logs during
migrations; filtering those out can help.
- HTTP API readiness (requires SSVAPIPort > 0):
    - Serving SSV API (INFO)
- Exporter/v2 tracing (archive mode only):
    - start duty tracer cache to disk evictor (INFO, startup)
    - evicted committee duty traces to disk (INFO, periodic)
    - evicted validator duty traces to disk (INFO, periodic)
    - evicted validator mappings to disk (INFO, periodic)
- Expect “using pebble db” (INFO) when the exporter is enabled (both modes).

## Appendix — Field Constraints

- **Validator public keys:** 96 hex chars (48‑byte BLS).
- **Committee IDs:** 64 hex chars (32 bytes).
- **Slots & indices:** non‑negative int64.
- **Roles enum:** `ATTESTER`, `AGGREGATOR`, `PROPOSER`, `SYNC_COMMITTEE`, `SYNC_COMMITTEE_CONTRIBUTION`.

---

_This is a pre‑release. APIs and models may evolve before v2.0.0. Please report issues and feedback._
