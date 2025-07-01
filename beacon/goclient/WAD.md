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


## License & Attribution

Portions of this feature are adapted from the [Vouch](https://github.com/attestantio/vouch) project:

> Copyright © The Vouch Project Authors
> Licensed under the Apache License, Version 2.0

## Changes Made

The following modifications and improvements were made compared to the original Vouch codebase:

- The *Block Root to Slot* mapping, populated from Consensus Client Head Events (used for the Attestation Data Scoring feature), is implemented differently. This decision was made to better integrate with the SSV codebase.
- Introduced a timeout for BeaconBlockHeader HTTP calls, set to one-fourth of "soft" timeout. This handles scenarios where the cache was not populated from Head events. This change was necessary because the Consensus Client's BeaconBlockHeader endpoint has proven to be highly unreliable, with long response times that negatively impacted the overall reliability of the feature.
- Implemented retries with delays when fetching *Block Root to Slot* from either the cache or the Consensus Client HTTP endpoint. This significantly improved reliability. The main motivation: Head events were occasionally received milliseconds after the Attestation Data fetch attempt, reducing scoring accuracy. Introducing retries with static delays and a timeout mitigated this issue.
- Refactored soft and hard timeout handling loops for improved clarity. Additional logging was added to monitor how frequently Attestation Data is overridden due to a higher score.
- Added `nil` checks for the Attestation Data response to improve safety.
- Added validation logic for BeaconBlockHeader responses.
- The `blockRootToSlotCache` eviction logic was reimplemented using an external library to better align with the SSV codebase's caching patterns.

## Acknowledgements

Special thanks to the [Vouch](https://github.com/attestantio/vouch) maintainers and contributors for their open-source work, which served as the foundation for this feature.