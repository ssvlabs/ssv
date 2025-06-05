# Weighted Attestation Data (WAD)

## Description

**Weighted Attestation Data (WAD)** is a feature designed to improve the correctness of attestations by utilizing all configured Beacon nodes for fetching and evaluating attestation data during the Beacon duty processing flow.

When enabled, the SSV Node will concurrently request attestation data from all configured Beacon nodes. Each response is then scored based on:

- **Source and Target Epochs** returned in the attestation data
- **Proximity of the Block Root to the Slot** for which the attestation data was requested

The response with the **highest score** is selected. This mechanism is expected to improve the accuracy of the attestation head votes over time.

## Trade-offs

Fetching attestation data is part of the consensus duty flow. With WAD enabled, this phase may take slightly longer, as it waits for multiple responses. However, the implementation includes several **safeguards** (e.g., soft/hard timeouts, retries) to avoid negative impact from unresponsive or slow Beacon nodes.

To fully benefit from this feature, the SSV Node **must** be configured with **more than one Beacon node**. Enabling WAD with a single node has no performance gain.

## Recommended Companion Feature

It is **recommended** (but not required) to enable **Parallel Submissions** alongside WAD to further enhance node performance. This will submit the same duties to all Beacon nodes concurrently, increasing the load on the Beacon nodes to submit the duties at the fastest rate possible.

Related configuration: `WITH_PARALLEL_SUBMISSIONS` (environment variable) or `eth2.WithParallelSubmissions` (in `config.yaml`).

## Configuration

WAD is **disabled by default**. To enable it and configure properly:

```yaml
eth2:
  WithWeightedAttestationData: true    # Enable WAD feature
  BeaconNodeAddr: http://localhost:5052;http://localhost:5053    # Multiple beacon nodes required
  WithParallelSubmissions: true    # Recommended for optimal performance
```

Or using environment variables:
```env
WITH_WEIGHTED_ATTESTATION_DATA=true
BEACON_NODE_ADDR=http://localhost:5052;http://localhost:5053
WITH_PARALLEL_SUBMISSIONS=true
```

Note: Multiple Beacon nodes are required for WAD to be effective. Parallel submissions are recommended but not required.
