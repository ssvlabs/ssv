# Real IBE Integration — Compatibility Analysis & Path Forward

This document records the empirical finding that **Option A (reuse the
validator's herumi-BLS threshold key as the IBE trust anchor) does not
compose with `drand/tlock`**, and lays out the implementation path
forward.

The decision was made in Phase 0 of [TASKS.md](TASKS.md) (Option A:
"reuse validator key"). The decision was correct *if* the cryptographic
primitives composed; this document explains why they don't and what
needs to change.

## The compatibility issue

Two BLS-12-381 implementations are involved:

- **`herumi/bls-eth-go-binary`** — SSV's existing library, used for all
  validator-share signing. SSV initialises it with
  `bls.SetETHmode(bls.EthModeDraft07)`, which sets the IETF Eth2
  Domain-Separation Tag (DST) for hash-to-curve operations:
  `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_`
- **`go.dedis.ch/kyber` (used by `drand/tlock`)** — Drand uses kyber's
  BLS-12-381 implementation with its own DST:
  `bls.DefaultDomainG2()`, which is *not* the Eth2 DST. Drand has
  multiple schemes (`ShortSigSchemeID`, `UnchainedSchemeID`,
  `SigsOnG1ID`); they differ in which curve carries signatures and in
  exact DST string, but none uses the Eth2 DST.

Both libraries produce BLS signatures over the same curve (BLS-12-381),
but with different DSTs the signatures are over different
hash-to-curve points. A herumi-produced signature on tag `T` does
*not* decrypt a tlock-produced ciphertext bound to tag `T`, because
the IBE encryption hashes `T` using kyber's DST and decryption verifies
the BLS sig against the same DST-derived point.

I confirmed this by reading
`$HOME/go/pkg/mod/github.com/drand/tlock@*/tlock.go::TimeLock` and
`TimeUnlock` — the encrypt path calls `ibe.EncryptCCAonG2(bls.NewBLS12381Suite()…, id, …)`
where `id` is `scheme.DigestBeacon(…)`, computed under kyber's DST.
Decryption verifies the BLS sig under the same DST. Eth2 sigs would
fail verification on the kyber side because they sign a *different*
point.

## What this means for Option A

Under Option A, operators would use their *existing validator share*
(herumi-format) to produce the BLS partial sig on a no-quorum tag,
aggregate to a full sig, and present that as the tlock decryption
key. The aggregate is correct as a herumi sig on the tag, but tlock's
verify step will reject it because the DST doesn't match.

There are three theoretical ways to make Option A work:

1. **Configure herumi to use drand's DST.** Not exposed by the
   `bls-eth-go-binary` Go bindings; would require patching the C
   library and modifying SSV's BLS-init code globally — and
   immediately breaks Eth2 signing for everything else SSV does.
   Not viable.

2. **Implement IBE directly on top of herumi**, using its hash-to-curve
   with the Eth2 DST. This means re-implementing the Boneh-Franklin
   IBE primitive from scratch on top of the herumi pairing operations
   — a non-trivial cryptographic engineering project.

3. **Patch tlock to accept arbitrary DSTs, then configure it to use
   the Eth2 DST.** Possible but requires upstream changes to tlock
   plus careful security review (DST flexibility introduces its own
   pitfalls).

None of these are appropriate for the prototyping scope.

## Recommended path forward: Option B

**Run a separate threshold BLS keypair for IBE**, distinct from the
validator's existing share. Operators carry two BLS secret shares:

- `ValidatorShare` (herumi) — used for signing block-roots and other
  Eth2-protocol outputs. Unchanged from current SSV.
- `IBEShare` (kyber) — used for signing IBE no-quorum tags. New.

The protocol code uses two `Signer` instances, one for value-signing
(invoked at Phase 2 onion construction with `ValidatorShare`) and one
for tag-signing (invoked when emitting `NonReceiptAttestation`s, with
`IBEShare`). Aggregating non-receipts produces a kyber-format
threshold sig, which is exactly what tlock needs for decryption.

### Required changes

The current `tbft.Signer` interface assumes a single primitive. Option
B requires splitting it:

```go
// What the protocol does today:
type Signer interface {
    SignPartial(share, msg) (Signature, error)
    AggregatePartials(partials) (Signature, error)
    VerifyPartial(pubKeyShare, msg, sig) bool
    VerifyAggregate(clusterPubKey, msg, sig) bool
}

// What Option B needs:
type ValueSigner = Signer  // for output sigs (herumi)
type TagSigner  = Signer   // for IBE-tag sigs (kyber)
```

Each `tbft.Instance` would hold both, and:

- `BuildOnion` uses `ValueSigner.SignPartial(validatorShare, value)` for
  per-layer value sigs (unchanged in shape; new dependency).
- `BuildNonReceipt` uses `TagSigner.SignPartial(ibeShare, tag)` for
  no-quorum-tag sigs.
- `Resolve`'s `tryDeriveNextLayerKey` uses
  `TagSigner.AggregatePartials(non_receipt_partials)` to derive the
  decryption key.
- `Resolve`'s `tryReconstructLayer` uses
  `ValueSigner.VerifyPartial`/`AggregatePartials` for the output sig.

The `Controller`/`Scheduler` API surface needs minor updates to thread
both signers and both share-types through.

### DKG cost

Option B needs a separate DKG ceremony to generate the cluster's IBE
keypair. SSV's existing operator-onboarding flow does a DKG for the
validator key; the natural integration is to extend that ceremony to
also produce IBE shares, distributed alongside the validator shares.
This is one-time per cluster.

### Bandwidth / runtime cost

Negligible:
- Operators sign two messages instead of one when emitting a
  non-receipt — both BLS sigs, both ~1 ms.
- Cluster public keys storage doubles (validator pubkey + IBE pubkey)
  but each is ~48 bytes.
- No additional rounds.

## What's been done; what's left

**Done so far:**

- Phase 1–4c protocol implementation with `SignerGatedIBE` (functionally
  correct access gate using BLS verification, but no cryptographic
  confidentiality).
- End-to-end multi-operator runtime test
  ([runner_test.go](../protocol/v2/ssv/runner/tbft/runner_test.go))
  proves the protocol works at runtime with realistic timing.
- This compatibility analysis.

**To do for production-ready IBE:**

- Add `kyber` and `drand/tlock` dependencies to SSV's `go.mod`.
- Introduce a second `Signer` field on `tbft.Instance` for IBE-tag
  signing. Update `BuildNonReceipt`, `Instance.tryDeriveNextLayerKey`,
  and `Controller`/`Scheduler` plumbing to thread it through.
- Implement `KyberSigner` in `protocol/v2/tbft/blsbackend/` (or a new
  `kyberbackend` subpackage) wrapping kyber's BLS operations.
- Implement `TLockIBE` wrapping `drand/tlock`'s `TimeLock`/`TimeUnlock`
  primitives, using the cluster's IBE pubkey (kyber-format) as the
  trust anchor.
- Integrate with SSV's existing DKG flow so cluster setup also
  produces IBE shares.
- Update `protocol/v2/ssv/runner/tbft/Controller` and `Scheduler` to
  accept both `ValidatorShare` and `IBEShare`.

**Until that lands**, `SignerGatedIBE` (or the existing `StubIBE`) is
the only available IBE backend. Both work for protocol-correctness
testing and for any deployment where confidentiality of the encrypted
partial signatures isn't a deployment concern. They are *not*
appropriate for a deployment where an adversary observing the wire
can extract the partial sigs (which is what real IBE prevents).

## Summary

The Phase 0 decision (Option A: reuse validator key) was based on
an unverified assumption that herumi-BLS sigs and tlock would
interoperate. Empirical reading of tlock's source confirms they do
not, due to DST mismatch. The right path is Option B (separate IBE
keypair), which requires a protocol-level refactor (two signers per
Instance) and a DKG-flow extension. The work is well-scoped but
substantial — appropriate for a focused implementation track once
the protocol semantics are settled (which they are, as of Phase 4d).
