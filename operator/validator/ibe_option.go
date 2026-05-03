package validator

// IBEUseOptionB toggles which IBE keypair the TBFT-IBE pipeline uses:
//
//   - false (Option A) — the validator's existing herumi BLS threshold
//     key serves as the IBE trust anchor via the DST trick (see
//     docs/IBE-INTEGRATION.md). KyberSigner consumes the operator's
//     validator share bytes; ClusterPubKey is the validator pubkey.
//     No separate DKG runs; the orchestrator is not constructed.
//   - true  (Option B) — a per-cluster IBE keypair is established by
//     an interactive Pedersen DKG (Phases A-G of
//     docs/TBFT-DKG-TASKS.md). KyberSigner consumes a DKG-derived
//     IBE share; ClusterPubKey is the cluster's IBE pubkey;
//     IBEPubKeyShares (computed from polyCommits) enable observe-time
//     NonReceipt-attestation verification.
//
// Both pipelines use the unified threshold qEnc = qV = 2f+1, so the
// protocol-level σ-quorum and NR-quorum checks are identical between
// modes. The mode choice affects only the key material, the trust
// anchor, and whether DKG infrastructure is wired up.
//
// Option B's machinery is fully implemented and tested (see Phase A-G
// in the doc) but kept dormant for the initial rollout — Option A
// reuses existing share infrastructure and avoids the DKG ceremony's
// startup cost. Flip this constant to switch; cluster-wide consistency
// (every operator in a cluster must agree on the value) is the
// operational requirement, enforced today by uniform binary
// deployment.
const IBEUseOptionB = false
