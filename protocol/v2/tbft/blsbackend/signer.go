// Package blsbackend provides concrete TBFT primitive implementations
// backed by herumi/bls-eth-go-binary — the same BLS library SSV's existing
// validator-share infrastructure uses.
//
// This package exists to bridge the abstract interfaces in protocol/v2/tbft
// (which know nothing about specific BLS libraries) with the concrete
// cryptography SSV operates with. It's not part of the TBFT core package
// because the core stays library-agnostic for testability and clarity.
//
// Under the chosen Option A (reuse the validator's existing threshold BLS
// key as the IBE trust anchor), this package's Signer is the production
// signer used both for value-signing (partial sigs that go inside Onions)
// and for tag-signing (partial sigs that aggregate into IBE decryption
// keys). The IBE primitive (tlock-backed) lives alongside in this package
// once integrated.
package blsbackend

import (
	"fmt"
	"sync"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
)

// initOnce ensures bls.Init is called exactly once per process. The herumi
// library is C-backed and re-initialization is wasteful (and may not be
// safe across all versions).
var initOnce sync.Once

func ensureInit() {
	initOnce.Do(func() {
		// Errors are intentionally ignored; matching SSV's existing
		// threshold.Init() pattern. Calling Init twice should be a no-op
		// in practice, but sync.Once is the safer guard.
		_ = bls.Init(bls.BLS12_381)
		_ = bls.SetETHmode(bls.EthModeDraft07)
	})
}

// BLSSigner is the herumi/bls-backed implementation of tbft.Signer.
//
// All inputs and outputs are serialized BLS bytes:
//
//   - share        : serialized bls.SecretKey bytes (operator's secret share)
//   - pubKeyShare  : serialized bls.PublicKey bytes (operator's pubkey share)
//   - clusterPubKey: serialized bls.PublicKey bytes (the master / validator pubkey)
//   - msg          : arbitrary bytes (signed via bls.SecretKey.SignByte / bls.Sign.VerifyByte)
//   - partial / sig: serialized bls.Sign bytes
type BLSSigner struct{}

// New creates a BLSSigner. Calls bls.Init once per process.
func New() *BLSSigner {
	ensureInit()
	return &BLSSigner{}
}

// SignPartial signs `msg` using `share` and returns a serialized partial
// signature. Each invocation deserializes the share fresh; if the SSV
// adapter wants to amortize, it can keep a parsed key alongside and call
// the herumi API directly. Per-call deserialization keeps this package
// trivially safe to use from multiple goroutines without sharing state.
func (s *BLSSigner) SignPartial(share []byte, msg []byte) (tbft.Signature, error) {
	if len(share) == 0 {
		return nil, fmt.Errorf("blsbackend: empty share")
	}
	if len(msg) == 0 {
		return nil, fmt.Errorf("blsbackend: empty message")
	}
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(share); err != nil {
		return nil, fmt.Errorf("blsbackend: deserialize share: %w", err)
	}
	sig := sk.SignByte(msg)
	if sig == nil {
		return nil, fmt.Errorf("blsbackend: SignByte returned nil")
	}
	return tbft.Signature(sig.Serialize()), nil
}

// AggregatePartials reconstructs a full BLS signature from partial signatures
// keyed by operator ID, using Lagrange interpolation. Any subset of size ≥ q
// (where q is the threshold the keys were generated for) yields the SAME
// reconstructed signature, regardless of which subset is used — this is the
// critical property TBFT relies on for cluster-wide consistency.
//
// The map keys are operator IDs; herumi's Recover uses these as the Shamir
// x-coordinates (encoded as bls.ID values). They must match the IDs used
// when the secret was split.
func (s *BLSSigner) AggregatePartials(partials map[tbft.OperatorID]tbft.Signature) (tbft.Signature, error) {
	if len(partials) == 0 {
		return nil, fmt.Errorf("blsbackend: no partials to aggregate")
	}

	sigVec := make([]bls.Sign, 0, len(partials))
	idVec := make([]bls.ID, 0, len(partials))

	for opID, p := range partials {
		sig := bls.Sign{}
		if err := sig.Deserialize(p); err != nil {
			return nil, fmt.Errorf("blsbackend: deserialize partial for op %d: %w", opID, err)
		}
		sigVec = append(sigVec, sig)

		var blsID bls.ID
		// herumi's bls.ID.SetDecString takes a decimal string of the
		// integer x-coordinate. SSV's existing share infrastructure uses
		// operator IDs directly as the x-coordinates, so we mirror that.
		if err := blsID.SetDecString(fmt.Sprintf("%d", opID)); err != nil {
			return nil, fmt.Errorf("blsbackend: set bls.ID for op %d: %w", opID, err)
		}
		idVec = append(idVec, blsID)
	}

	recovered := bls.Sign{}
	if err := recovered.Recover(sigVec, idVec); err != nil {
		return nil, fmt.Errorf("blsbackend: BLS Recover: %w", err)
	}
	return tbft.Signature(recovered.Serialize()), nil
}

// VerifyPartial checks that `partial` is a valid BLS signature on `msg`
// under the public key `pubKeyShare`. Returns false on any deserialization
// or verification failure.
func (s *BLSSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial tbft.Signature) bool {
	if len(pubKeyShare) == 0 || len(msg) == 0 || len(partial) == 0 {
		return false
	}
	pk := bls.PublicKey{}
	if err := pk.Deserialize(pubKeyShare); err != nil {
		return false
	}
	sig := bls.Sign{}
	if err := sig.Deserialize(partial); err != nil {
		return false
	}
	return sig.VerifyByte(&pk, msg)
}

// VerifyAggregate checks that `sig` is a valid full BLS signature on `msg`
// under the cluster's master public key (the validator's pubkey under
// Option A). Returns false on any deserialization or verification failure.
func (s *BLSSigner) VerifyAggregate(clusterPubKey []byte, msg []byte, sig tbft.Signature) bool {
	if len(clusterPubKey) == 0 || len(msg) == 0 || len(sig) == 0 {
		return false
	}
	pk := bls.PublicKey{}
	if err := pk.Deserialize(clusterPubKey); err != nil {
		return false
	}
	s2 := bls.Sign{}
	if err := s2.Deserialize(sig); err != nil {
		return false
	}
	return s2.VerifyByte(&pk, msg)
}
