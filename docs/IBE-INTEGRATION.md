# Real IBE Integration — Status: Working

This document describes the working implementation of real cryptographic
IBE for TBFT, using existing herumi-BLS validator shares without a
separate DKG. Phase 4b's "production cryptographic IBE" track is
substantively done.

The original Phase 0 decision (Option A: reuse the validator's herumi-BLS
threshold key as the IBE trust anchor) **is correct** — but its
implementation requires a different cryptographic technique than I
initially recognised. The technique is the "DST trick": same secret
share, two different Domain Separation Tags. This document explains the
technique, the implementation, and the proof that it works.

## The DST trick

Two BLS-12-381 implementations are involved:

- **`herumi/bls-eth-go-binary`** — SSV's existing library, used for
  validator-share signing. Initialised with `bls.SetETHmode(bls.EthModeDraft07)`,
  which sets the Eth2 DST: `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_`.
- **`go.dedis.ch/kyber` + `github.com/drand/kyber-bls12381`** — drand's
  kyber-side BLS implementation, used by the IBE primitives in
  `github.com/drand/kyber/encrypt/ibe`. Default DST:
  `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_NUL_`.

Critically: a herumi BLS share `s_i` and a kyber BLS scalar are **the
same kind of mathematical object** — a scalar in BLS-12-381's prime
field `F_r`. The validator's master secret `s` is a scalar; the share
`s_i = P(i)` is `P` evaluated at operator-index `i` for some Shamir
polynomial `P` over `F_r`.

What differs between herumi and kyber isn't the key material — it's the
hash-to-curve step that produces the message-point. When operator `i`
signs message `m`:

```
herumi sig:  s_i · H_eth(m)     where H_eth uses the Eth2 DST (POP_ suffix)
kyber sig:   s_i · H_kyber(m)   where H_kyber uses the drand DST (NUL_ suffix)
```

Same scalar, different curve point. Lagrange interpolation — the
threshold reconstruction step — is a property of the **scalar field**,
not the hash-to-curve scheme. So aggregating 2f+1 herumi sigs gives
`s · H_eth(m)`; aggregating 2f+1 kyber sigs from the same shares gives
`s · H_kyber(m)`. Both are valid threshold signatures, just under
different DSTs.

Crucially, **drand/tlock encrypts to whatever pubkey you give it** and
verifies decryption by checking that the supplied "decryption key" is a
valid BLS sig (under tlock's chosen scheme/DST) on the encryption tag.
If we give tlock the validator's pubkey `s · G`, and supply as the
decryption key the kyber-aggregated sig from herumi shares, the
verification step works: `s · G` is the same group element regardless
of which library produced it; `s · H_kyber(tag)` is exactly what tlock
expects.

## Implementation

Three files in [protocol/v2/tbft/blsbackend/](../protocol/v2/tbft/blsbackend/):

- [kyber_conversion.go](../protocol/v2/tbft/blsbackend/kyber_conversion.go)
  — `HerumiShareToKyberScalar` and `HerumiPubkeyToKyberG1Point`. Both
  libraries serialise BLS-12-381 scalars and G1 points byte-equivalently
  per the IETF/Eth2 standard, so these are direct pass-throughs with
  length checks. Verified by
  [kyber_conversion_test.go](../protocol/v2/tbft/blsbackend/kyber_conversion_test.go):
  - Scalars round-trip with byte-equality.
  - Pubkeys round-trip with byte-equality.
  - Mathematical consistency: `HerumiPubkeyToKyberG1Point(pk) ==
    HerumiShareToKyberScalar(sk) · G1`.
  - Threshold consistency: 2f+1 herumi shares interpreted as kyber
    scalars Lagrange-interpolate to the master scalar.

- [kyber_signer.go](../protocol/v2/tbft/blsbackend/kyber_signer.go) —
  `KyberSigner` implements `tbft.Signer` using kyber-bls12381. Inputs
  are herumi-format bytes (shares and pubkey shares); outputs are
  kyber-format G2 partial signatures. Aggregation via Lagrange
  interpolation in kyber's scalar field.
  [kyber_signer_test.go](../protocol/v2/tbft/blsbackend/kyber_signer_test.go)
  proves the contract: round-trip, "any 2f+1 subset yields the same
  aggregate", per-partial verification, below-quorum rejection.

- [tlock_ibe.go](../protocol/v2/tbft/blsbackend/tlock_ibe.go) —
  `TLockIBE` implements `tbft.ThresholdIBE` using
  `github.com/drand/kyber/encrypt/ibe`. Hybrid encryption: a fresh
  AES-256 key is wrapped under IBE; the actual plaintext is AES-GCM-
  encrypted under that key. The decryption key TLockIBE expects is
  a kyber-format G2 BLS sig on the tag. Wire format is versioned
  length-prefixed.
  [tlock_ibe_test.go](../protocol/v2/tbft/blsbackend/tlock_ibe_test.go)
  proves the round-trip works with herumi-share-derived kyber sigs,
  different quorum subsets decrypt identically, wrong-tag keys fail,
  below-quorum keys fail.

## Protocol integration

The TBFT protocol's `Instance` now holds two `Signer` fields:

- `signer` — value-signer, used to sign Phase-2 onion contents (the
  partial sigs on candidate values). For SSV this is `BLSSigner`
  (herumi, Eth2 DST).
- `tagSigner` — tag-signer, used to sign Phase-1 no-quorum tags
  (`NonReceiptAttestation`s) and to aggregate them in Phase-3
  decryption-key derivation. For real IBE this is `KyberSigner` (kyber,
  drand DST).

Constructed via `tbft.NewInstanceWithTagSigner(...)`. The original
`tbft.NewInstance(...)` is preserved as a backward-compatible shim that
sets `tagSigner = signer` (suitable when paired with `SignerGatedIBE` or
`StubIBE`, where one DST suffices).

Both signers consume the **same operator share** — the existing
herumi-format secret. No share duplication. No DKG ceremony changes.

## Capstone test

The integration capstone is
[end_to_end_real_ibe_test.go](../protocol/v2/tbft/blsbackend/end_to_end_real_ibe_test.go)
— `TestEndToEndRealIBE_LayerFallthrough`. It exercises:

- 7-operator cluster, real threshold-split BLS keys via SSV's existing
  `utils/threshold.Create`.
- Two signers: `BLSSigner` for value-signing, `KyberSigner` for
  tag-signing. Same shares. No new key material.
- Real `TLockIBE` for IBE encryption/decryption.
- Layer-0 leader silent → all operators emit non-receipts → kyber-
  aggregated non-receipt sig decrypts layer-1 onion contents → layer 1
  reaches positive quorum → all operators output the same reconstructed
  Eth2-format validator signature on layer 1's value.

Test passes. The reconstructed signature byte-equals what the master
herumi key would sign directly — i.e. it is a valid Eth2 BLS signature
that SSV's beacon-submission code accepts unchanged.

## Security argument

Domain Separation Tags exist precisely so the same secret can be safely
used with multiple sub-protocols. Signing the same message under different
DSTs produces signatures that are independent in the cryptographic sense:
an adversary observing an Eth2 sig on `m1` cannot derive a kyber sig on
`m2` (or vice versa), and discrete-log security of the share is
unaffected by either kind of signature.

The standard analysis: as long as the two DSTs are distinct strings (they
are — "POP_" vs "NUL_" suffixes alone differentiate them), the two
signing oracles are independent random oracles to an adversary even
though they share a secret. This is exactly the threat model DSTs are
designed to handle.

There's one engineering tradeoff worth being explicit about: **using the
validator's secret for non-Eth2 purposes does increase the attack surface
on that secret**. If a bug in the kyber-BLS code path leaks the scalar
(e.g. side-channel, malformed-input parsing bug), the validator key is
compromised. With separate DKG keys, a kyber-side bug only compromises
IBE decryption capability, not block-signing capability. This is the
*only* tradeoff — there's no cryptographic weakness from the DST trick
itself.

For prototyping and devnet validation, the trade is fine. For mainnet
deployment with the highest security bar, evaluating whether to use this
shared-share approach vs. running a separate DKG ceremony is the
operational call. The protocol code supports both: pass `tagSigner = nil`
to `NewInstanceWithTagSigner` and you fall back to the shared-share
behaviour; pass a separately-keyed `KyberSigner` (using IBE-only shares
from a parallel DKG) and the protocol uses those distinct shares for
tag-signing. The choice is at instance construction, not in the protocol
code itself.

## What this means for the implementation plan

Phase 4b's "production cryptographic IBE" track was originally documented
as a substantial protocol refactor + DKG-flow extension. With the DST
trick, it reduces to:

- ✅ Add kyber + drand IBE deps to `go.mod` (done, single PR's worth)
- ✅ Implement `KyberSigner` and `TLockIBE` (done)
- ✅ Implement byte conversions herumi ↔ kyber (done)
- ✅ Add `Instance.tagSigner` field with backward-compat default (done)
- ✅ End-to-end test proving the full pipeline works with real IBE (done)
- ⏳ Update `Controller`/`Scheduler` constructor options to optionally
  accept a separate `TagSigner` parameter, threading it through to
  `NewInstanceWithTagSigner` (small follow-up).

That's it. No DKG changes, no operator-onboarding changes, no separate
share storage. The runner-integration work in Phase 4d (modifying
`proposer.go`) is unchanged — it just gets passed both signers at
construction time and the rest is automatic.

## Test summary

```
$ go test ./protocol/v2/tbft/... ./protocol/v2/ssv/runner/tbft/... -count=1 -race
ok  github.com/ssvlabs/ssv/protocol/v2/tbft
ok  github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend
ok  github.com/ssvlabs/ssv/protocol/v2/tbft/wire
ok  github.com/ssvlabs/ssv/protocol/v2/ssv/runner/tbft
```

All packages green, race-detector clean. Total test count well above
140 across the four packages, including the capstone end-to-end test
that exercises the entire DST-trick stack.
