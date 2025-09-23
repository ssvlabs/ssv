# SSV Exporter v2 — Pre‑Release Notes (v2.0.0‑rc.1)

- **Audience:** SSV node operators and clients integrating with the exporter API.  
- **Focus:** configuration (files/flags/env), API surface (HTTP + WebSocket), compatibility, and upgrade steps.
- **Compatibility:** v2 keeps legacy clients working. 

---

## Overview

Exporter v2 introduces **full duty tracing** (consensus rounds, pre/post partial signatures), **committee‑level views**, richer filtering, OpenAPI definitions, and a clearer configuration model with **two operating modes**:

### Standard mode

Setting `Mode: standard` keeps the legacy behavior:
- Data is pruned automatically
- The HTTP API *fails fast* and aborts on first error
- The legacy `/decideds` payload is preserved

### Archive mode

Setting `Mode: archive` enables the full extent of v2.

Compared to `Mode: standard`:
- Data is permanently saved to disk
- The HTTP API implements *best-effort processing* and returns all results that could be processed
- The legacy `/decideds` payload is enriched with a new `errors` field to support best-effort processing

In addition, `Mode: archive` also enables:
- Full duty tracing: consensus messages are recorded on disk and exposed via the API
- These traces are exposed via two new API endpoints: `/traces/validator` and `/traces/committee`

**Best‑effort** processing: the exporter returns partial results (`data`) alongside non‑fatal issues (`errors`) in an envelope response. If a request yields **no valid data** and **at least one meaningful error**, an HTTP error is returned instead of an empty success.

---

## Configuration file

The configuration file schema was updated in favor of a clearer configuration model.

Changes:
- **New top‑level block:** `exporter.Enabled`, `exporter.Mode`, `exporter.RetainSlots`.
- **Deprecated:** `ssv.ValidatorOptions.Exporter: true` (replace with the block above). Any previous example configs using that key must be updated.

> If your deployment uses environment variable overrides for YAML keys, use your usual `FOO__BAR` convention (no change introduced by v2).

### Minimal working config: archive mode

```yaml
global:
  LogLevel: debug  # please use debug on first run

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

SSVAPIPort: 16000        # enables HTTP API, optional 
WebSocketAPIPort: 16001  # enables WS API, optional
WithPing: true
```

### Minimal working config: standard mode

```yaml
global:
  LogLevel: debug  # please use debug on first run

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
  Mode: standard
  RetainSlots: 50400     # v1-compatible way to control memory use

SSVAPIPort: 16000        # enables HTTP API, optional 
WebSocketAPIPort: 16001  # enables WS API, optional
WithPing: true
```

---

## HTTP API overview (updated)

Please refer to the new [OpenApi spec](https://github.com/ssvlabs/ssv/tree/stage/docs/API) (`docs/API/ssvnode.openAPI.yaml|json`) for details. Below is a high-level overview of the new HTTP API endpoints.

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

curl -s -X POST http://localhost:16000/v1/exporter/traces/validator   -H 'Content-Type: application/json'   -d '{"from":123456,"to":123460,"roles":["ATTESTER"],"pubkeys":["<96-char hex>"],"indices":[123]}'
```

Response data schema: [ValidatorTrace](https://github.com/ssvlabs/ssv/blob/164e25cb6462759f98063fa32dd092f0dc4439f4/docs/API/ssvnode.openAPI.json#L996)


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
curl -s "http://localhost:16000/v1/exporter/traces/committee?from=123456&to=123460&committeeIDs=<64-char hex>"

curl -s -X POST http://localhost:16000/v1/exporter/traces/committee   -H 'Content-Type: application/json'   -d '{"from":123456,"to":123460,"committeeIDs":["<64-char hex>"]}'
```

Response data schema: [CommitteeTrace](https://github.com/ssvlabs/ssv/blob/164e25cb6462759f98063fa32dd092f0dc4439f4/docs/API/ssvnode.openAPI.json#L630)

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
curl -s "http://localhost:16000/v1/exporter/decideds?from=123456&to=123460&roles=ATTESTER&pubkeys=<96-char hex>"

curl -s -X POST http://localhost:16000/v1/exporter/decideds   -H 'Content-Type: application/json'   -d '{"from":123456,"to":123460,"roles":["ATTESTER"],"pubkeys":["<96-char hex>"]}'
```

Response data schema: [TraceDecideds](https://github.com/ssvlabs/ssv/blob/164e25cb6462759f98063fa32dd092f0dc4439f4/docs/API/ssvnode.openAPI.json#L972)

### API requests: field constraints

- **Validator public keys:** 96 hex chars (48‑byte BLS).
- **Committee IDs:** 64 hex chars (32 bytes).
- **Slots & indices:** non‑negative int64.
- **Roles enum:** `ATTESTER`, `AGGREGATOR`, `PROPOSER`, `SYNC_COMMITTEE`, `SYNC_COMMITTEE_CONTRIBUTION`.

---

## WebSocket API overview (unchanged)

No changes were made to the WebSocket API. As before, when `WebSocketAPIPort` is configured and the exporter is enabled:

- `ws://<host>:<port>/stream` — *decided (quorum) events* published once per `(validator, role, root)` with a *1‑minute de‑duplication window*.
- `ws://<host>:<port>/query` — retained for ad‑hoc lookups (internal format), unchanged from v1.

---

## Operational Notes

- Persisting detailed traces increases CPU, memory, disk, and P2P usage.
- No safety was implemented against large ranges in API requests and Out-of-Memory errors are possible: users are expected to call the API with reasonable ranges that the node can handle (exact limits depend on available RAM to hold results).
- DB migration may take a long time to process and garbage collect. Enable `debug` logs on first run.

---

## Upgrade Guide

1. **Upgrade** your node to a build that includes Exporter v2.
2. **Migrate config:**
   - Remove `ssv.ValidatorOptions.Exporter: true` if present.
   - Add the new `exporter` block with `Enabled` and `Mode` (and `RetainSlots` for standard).
   - Set an API port
   - Please refer to our [minimal configuration examples](#Configuration-file) for a working example.
3. **Restart** the node with archive mode enabled.
4. **Validate**: probe `/v1/exporter/decideds` and (if archive) `/v1/exporter/traces/validator`; connect to `/stream` to observe live decided events.

### Configuration file upgrade

**Deprecated**
```
ssv:
  ValidatorOptions:
    Exporter: true
    ExporterRetainSlots: 50400
```

**New**
```
exporter:
  Enabled: true
  Mode: standard # possible values: standard, archive
  RetainSlots: 50400
```
---

## Troubleshooting

### Using the logs

- Enable DEBUG logs for the first run of exporter v2 to see migration and GC progress.
- Database migrations can take time. Before upgrading, back up your database. Look for this INFO log to confirm completion:
    - migration completed
- Post‑migration garbage collection (GC) can take even longer. Look for this DEBUG log to confirm GC completion:
    - post-migration garbage collection completed
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

---

_This is a pre‑release. APIs and models may evolve before v2.0.0. Please report issues and feedback._
