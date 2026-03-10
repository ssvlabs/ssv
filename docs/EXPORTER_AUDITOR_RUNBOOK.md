# Exporter Auditor Runbook (trace ↔ schedule mismatches)

This runbook helps debug cases where `/v1/exporter/traces/committee` returns **P2P traces** (`data`) for validator indices and/or SSV committees that do **not** appear in the response `schedule`.

Key definitions:
- **`data`**: what we observed on the P2P wire (must never be filtered).
- **`schedule`**: what duties were scheduled/requested by the beacon chain (what we *expected* to happen).
- **“committee” in `/traces/committee`**: an **SSV committee** (cluster of operators for a validator), not a beacon-chain committee.

The auditor is **log-first**: every mismatch is logged with high-cardinality evidence. A small, capped on-disk findings store is used for longer history and ad-hoc querying via HTTP.

## Enablement

The auditor currently runs only when the node is in exporter **archive** mode.

Environment variables:
- `EXPORTER=true`
- `EXPORTER_MODE=archive`
- `EXPORTER_AUDITOR=true` (default: `false`)
- `EXPORTER_AUDITOR_RPC_FALLBACK=true` (default: `true`)
- `EXPORTER_AUDITOR_DELAY_SLOTS=4` (default: `4`, audits slot *S* at about *S+4*)

Sanity check:
- `GET /v1/exporter/auditor/status`

## Primary UX: Loki/Grafana logs

Two log lines are the main entry points:
- `auditor mismatch` (WARN): one per detected mismatch, always emitted even if the store is capped/unavailable.
- `auditor summary` (INFO): one per `(slot, reason)` with counters (emitted/stored/dropped, RPC usage, role counts).

Suggested LogQL patterns (adjust labels/pod to your environment):

1) Find mismatches in a time window:
```
{cluster="stage", pod=~"ssv-node-exporter-mainnet-1-.*"} |= "auditor mismatch"
```

2) Filter to a specific reason and validator index:
```
{cluster="stage", pod=~"ssv-node-exporter-mainnet-1-.*"} |= "auditor mismatch"
| json
| reason="SCHEDULE_MISSING_INDEX"
| validator_index="843156"
```

3) Correlate a single mismatch:
- Use `finding_id` (deterministic fingerprint; present even when persistence is capped).
- If `stored=true`, use `finding_key` to find the persisted record range quickly (see HTTP queries below).

Important fields in `auditor mismatch`:
- `slot`, `epoch`, `reason`, `role`, `validator_index`, `committee_id_observed`
- `schedule_*` (`schedule_read_ok`, `schedule_size`, `schedule_has_index`, `schedule_has_role`, `schedule_mask_bits`)
- `registry_*` (`registry_validator_known`, `committee_id_registry_expected`, …)
- `link_*` (`link_present`, `committee_id_linked`, …)
- `rpc_*` (`rpc_enabled`, `rpc_used`, `rpc_ok`, `rpc_error`, `rpc_expected_slot`)
- pipeline evidence: `duty_fetch_*`, `schedule_compute_*`, `schedule_job_drops`
- observed wire timing: `received_min_ms`, `received_max_ms`

## Secondary UX: findings store (HTTP)

Endpoints:
- `GET /v1/exporter/auditor/status`
- `GET|POST /v1/exporter/auditor/findings`
- `GET|POST /v1/exporter/auditor/summary`

The findings store is **capped** at **max 10 findings per `(slot, reason)`**. Capped findings still appear in logs with `stored=false` and `drop_why=cap_reached`.

Common curl recipes:

1) Latest findings (defaults to the last ~256 audited slots):
```
curl -sS 'http://HOST:PORT/v1/exporter/auditor/findings' | jq .
```

2) Single-slot deep dive (best for correlating with `/traces/committee` complaints):
```
curl -sS 'http://HOST:PORT/v1/exporter/auditor/findings?from=13772863&to=13772863&limit=1000' | jq .
```

3) Filter by validator index:
```
curl -sS 'http://HOST:PORT/v1/exporter/auditor/findings?lastN=2048&validatorIndex=843156' | jq .
```

4) Filter by SSV committee ID (32-byte hex, no 0x prefix):
```
curl -sS 'http://HOST:PORT/v1/exporter/auditor/findings?lastN=2048&committeeID=0123...abcd' | jq .
```

5) Filter by reason, paginate with cursor:
```
curl -sS 'http://HOST:PORT/v1/exporter/auditor/findings?lastN=4096&reason=SCHEDULE_READ_FAILED&limit=100&order=desc' | jq .
```
Then reuse the last item’s `key` as `cursor=<slot>/<reason>/<seq>` to fetch the next page.

6) Summaries (fastest way to see what’s happening per reason):
```
curl -sS 'http://HOST:PORT/v1/exporter/auditor/summary?lastN=2048' | jq .
```

## Beacon schedule verification (when needed)

When the auditor uses `EXPORTER_AUDITOR_RPC_FALLBACK=true`, it attempts to confirm expected duties directly from the beacon node for mismatching indices and records the outcome (`rpc_used`, `rpc_ok`, `rpc_error`, and role-specific details such as `rpc_expected_slot`).

If you need to independently verify beacon duties:
1) Determine `epoch` for a `slot`:
   - Mainnet: `epoch = floor(slot / 32)`
2) Query a beacon node:
   - Get beacon services (requires VNet access):
     - `tsh kubectl get svc -n ethereum-clients | grep beacon`
   - Then port-forward a beacon service locally and query it.

Example beacon API calls (typical endpoints; adjust host/port and auth as needed):
- Attester duties (POST):
  - `/eth/v1/validator/duties/attester/{epoch}` with body `{"indices":["843156","843157"]}`
- Sync committee duties (POST):
  - `/eth/v1/validator/duties/sync/{epoch}` with body `{"indices":["843156","843157"]}`

## Reason codes: meaning and next actions

Use `reason` to decide what to check next; always start from the `auditor mismatch` log entry for full evidence.

- `SCHEDULE_READ_FAILED` / `LINKS_READ_FAILED`
  - Meaning: local schedule or link table read failed for that slot.
  - Next: check exporter DB health, disk, pruning, and concurrent writes; correlate with `schedule_read_error` / `links_read_error`.

- `SCHEDULE_NOT_COMPUTED` / `SCHEDULE_COMPUTE_FAILED` / `SCHEDULE_JOB_DROPPED`
  - Meaning: schedule wasn’t available when audit ran, schedule compute failed, or schedule jobs were dropped.
  - Next: check `schedule_compute_*`, `schedule_job_drops`, and duty fetch evidence; check node load/backpressure.

- `SCHEDULE_BEFORE_DUTIES_READY` / `DUTY_FETCH_FAILED`
  - Meaning: audit ran before duties were fetched/ready, or duty fetch failed.
  - Next: check `duty_fetch_*` and beacon client health; look for beacon timeouts and retry behavior.

- `DUTY_STORE_INCOMPLETE`
  - Meaning: local duty store suggests the index should have a duty, but schedule is missing it.
  - Next: suspect a schedule fill bug, pruning/race, or slot boundary issues; verify with RPC fallback and beacon logs.

- `RPC_FALLBACK_FAILED` / `RPC_FALLBACK_SKIPPED`
  - Meaning: beacon RPC verification failed, or was skipped due to per-slot RPC caps.
  - Next: check beacon connectivity/rate limiting; reduce mismatch volume (root cause) if caps are hit.

- `TRACE_SLOT_MISATTRIBUTED`
  - Meaning: wire traces are likely being bucketed under the wrong slot.
  - Next: inspect `received_min_ms` / `received_max_ms` and correlate with real slot boundaries; suspect timestamping or slot derivation issues.

- `REGISTRY_INDEX_NOT_FOUND`
  - Meaning: validator index unknown to local registry at audit time.
  - Next: check registry sync, operator state, and whether the validator was recently added/removed.

- `COMMITTEE_LINK_MISSING` / `COMMITTEE_LINK_MISMATCH` / `REGISTRY_COMMITTEE_MISMATCH`
  - Meaning: we observed wire traces for a committee, but the local link table/registry mapping doesn’t match.
  - Next: suspect committee mapping / link persistence bugs or stale registry metadata; use `committee_id_linked` and `committee_id_registry_expected`.

- `UNEXPECTED_WIRE_TRACE` / `ROLE_CLASSIFICATION_SUSPECT`
  - Meaning: we saw wire messages that appear “unexpected” relative to schedule/mapping, or the role classification may be wrong.
  - Next: focus on message classification paths; correlate signers and committee mapping; look for duplicate/late/replayed messages.

## Metrics and alerting

Auditor emits metrics under the `ssv.dutytracer.auditor.*` namespace:
- `ssv.dutytracer.auditor.findings.total{reason=...}`
- `ssv.dutytracer.auditor.findings.dropped{reason=...,drop_why=...}`
- `ssv.dutytracer.auditor.last_audited_slot`
- `ssv.dutytracer.auditor.rpc.attester.requests`, `.errors`
- `ssv.dutytracer.auditor.rpc.sync.requests`, `.errors`

Recommended alerts:
- sustained non-zero `findings.total` for reasons that indicate systemic errors (`SCHEDULE_READ_FAILED`, `SCHEDULE_JOB_DROPPED`, `TRACE_SLOT_MISATTRIBUTED`)
- spike in `findings.dropped{drop_why="cap_reached"}` (means store isn’t capturing full diversity; rely on logs + increase cap only if necessary)

