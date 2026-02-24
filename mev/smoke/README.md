# MEV Builder Endpoint Smoke Harness (local, docker-compose)

This is a local smoke harness to exercise the SSV Builder API endpoint with **real HTTP calls** between:

- `mev-mock-relay` (one or more mock relays)
- `mev-builder-smoke` (runs `mev/builderendpoint` server with configured relays)
- `mev-smoke-client` (calls the builder endpoint and asserts basic semantics)

It is intentionally minimal and meant for quick iteration while implementing Steps 5–6 (prefetch/head-awareness).

## Run

From the repo root:

```sh
docker compose -f mev/smoke/docker-compose.yml up --build --abort-on-container-exit --exit-code-from smoke
```

Expected result: the `smoke` container exits `0` after validating:

- `GET /eth/v1/builder/status` is `200`
- `GET /eth/v1/builder/header/...` returns a bid and selects the highest relay value
- `POST /eth/v1/builder/blinded_blocks` returns `200` and a Deneb response envelope
- `POST /eth/v1/builder/validators` returns `200`

