package ssv

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// PartialSigContainer collects partial signatures during post-consensus.
// Signatures are indexed by validator, signing root, and operator.
type PartialSigContainer struct {
	// signaturesMu protects access to the Signatures map to prevent concurrent read/write access
	// from multiple goroutines
	signaturesMu sync.RWMutex
	// Signature map: validator index -> signing root -> operator id (signer) -> signature
	Signatures map[phase0.ValidatorIndex]map[[32]byte]map[spectypes.OperatorID]spectypes.Signature
	// Quorum is the number of min signatures needed for quorum
	Quorum uint64
}

// NewPartialSigContainer creates a container that collects partial signatures until quorum.
func NewPartialSigContainer(quorum uint64) *PartialSigContainer {
	return &PartialSigContainer{
		Quorum:     quorum,
		Signatures: make(map[phase0.ValidatorIndex]map[[32]byte]map[spectypes.OperatorID]spectypes.Signature),
	}
}

// AddSignature stores a partial signature if one does not already exist for the signer.
func (ps *PartialSigContainer) AddSignature(sigMsg *spectypes.PartialSignatureMessage) {
	ps.signaturesMu.Lock()
	defer ps.signaturesMu.Unlock()

	if ps.Signatures[sigMsg.ValidatorIndex] == nil {
		ps.Signatures[sigMsg.ValidatorIndex] = make(map[[32]byte]map[spectypes.OperatorID]spectypes.Signature)
	}
	if ps.Signatures[sigMsg.ValidatorIndex][sigMsg.SigningRoot] == nil {
		ps.Signatures[sigMsg.ValidatorIndex][sigMsg.SigningRoot] = make(map[spectypes.OperatorID]spectypes.Signature)
	}
	m := ps.Signatures[sigMsg.ValidatorIndex][sigMsg.SigningRoot]

	if m[sigMsg.Signer] == nil {
		m[sigMsg.Signer] = make([]byte, 96)
		copy(m[sigMsg.Signer], sigMsg.PartialSignature)
	}
}

// HasSignature returns true if a signature exists for the given signer and signing root.
func (ps *PartialSigContainer) HasSignature(validatorIndex phase0.ValidatorIndex, signer spectypes.OperatorID, signingRoot [32]byte) bool {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	return ps.Signatures[validatorIndex][signingRoot][signer] != nil
}

// GetSignature returns the signature for a given root and signer.
func (ps *PartialSigContainer) GetSignature(validatorIndex phase0.ValidatorIndex, signer spectypes.OperatorID, signingRoot [32]byte) (spectypes.Signature, error) {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	sig := ps.Signatures[validatorIndex][signingRoot][signer]
	if sig == nil {
		return nil, fmt.Errorf("no signature for validator %d root %x signer %d", validatorIndex, signingRoot, signer)
	}
	return sig, nil
}

// GetSignatures returns a copy of the signature map for a given root.
func (ps *PartialSigContainer) GetSignatures(validatorIndex phase0.ValidatorIndex, signingRoot [32]byte) map[spectypes.OperatorID]spectypes.Signature {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	signatures := ps.Signatures[validatorIndex][signingRoot]
	if signatures == nil {
		return nil
	}

	// Return a copy to avoid external mutation and data races
	signaturesCopy := make(map[spectypes.OperatorID]spectypes.Signature, len(signatures))
	maps.Copy(signaturesCopy, signatures)

	return signaturesCopy
}

// Remove deletes a signer from the signature map.
func (ps *PartialSigContainer) Remove(validatorIndex phase0.ValidatorIndex, signer uint64, signingRoot [32]byte) {
	ps.signaturesMu.Lock()
	defer ps.signaturesMu.Unlock()

	signers := ps.Signatures[validatorIndex][signingRoot]
	if signers == nil {
		return
	}
	delete(signers, signer)
}

// ReconstructSignature aggregates collected partial sigs and verifies the result.
func (ps *PartialSigContainer) ReconstructSignature(root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	rootSigs := ps.Signatures[validatorIndex][root]
	if rootSigs == nil {
		return nil, fmt.Errorf("no signatures for validator %d root %x", validatorIndex, root)
	}

	operatorsSignatures := make(map[uint64][]byte, len(rootSigs))
	for operatorID, sig := range rootSigs {
		operatorsSignatures[operatorID] = sig
	}
	signature, err := threshold.ReconstructSignatures(operatorsSignatures)
	if err != nil {
		return nil, fmt.Errorf("reconstruct signatures: %w", err)
	}

	// Copy to avoid cgo Go pointer to Go pointer issue
	validatorPubKeyCopy := make([]byte, len(validatorPubKey))
	copy(validatorPubKeyCopy, validatorPubKey)

	if err := types.VerifyReconstructedSignature(signature, validatorPubKeyCopy, root); err != nil {
		return nil, fmt.Errorf("verify reconstructed signature: %w", err)
	}
	return signature.Serialize(), nil
}

// HasQuorum returns true if enough signatures exist for the given root.
func (ps *PartialSigContainer) HasQuorum(validatorIndex phase0.ValidatorIndex, root [32]byte) (
	ok bool,
	signers []spectypes.OperatorID,
) {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	rootSigs := ps.Signatures[validatorIndex][root]

	return uint64(len(rootSigs)) >= ps.Quorum, slices.Collect(maps.Keys(rootSigs))
}

// MarshalJSON preserves the hex-string signing root format for backward-compatible JSON.
func (ps *PartialSigContainer) MarshalJSON() ([]byte, error) {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	type jsonContainer struct {
		Signatures map[phase0.ValidatorIndex]map[string]map[spectypes.OperatorID]spectypes.Signature
		Quorum     uint64
	}

	jc := jsonContainer{
		Quorum:     ps.Quorum,
		Signatures: make(map[phase0.ValidatorIndex]map[string]map[spectypes.OperatorID]spectypes.Signature, len(ps.Signatures)),
	}

	for vi, roots := range ps.Signatures {
		jc.Signatures[vi] = make(map[string]map[spectypes.OperatorID]spectypes.Signature, len(roots))
		for root, ops := range roots {
			jc.Signatures[vi][hex.EncodeToString(root[:])] = ops
		}
	}

	return json.Marshal(jc)
}

// UnmarshalJSON decodes hex-string signing roots back to [32]byte keys.
func (ps *PartialSigContainer) UnmarshalJSON(data []byte) error {
	type jsonContainer struct {
		Signatures map[phase0.ValidatorIndex]map[string]map[spectypes.OperatorID]spectypes.Signature
		Quorum     uint64
	}

	var jc jsonContainer
	if err := json.Unmarshal(data, &jc); err != nil {
		return err
	}

	ps.Quorum = jc.Quorum
	ps.Signatures = make(map[phase0.ValidatorIndex]map[[32]byte]map[spectypes.OperatorID]spectypes.Signature, len(jc.Signatures))

	for vi, roots := range jc.Signatures {
		ps.Signatures[vi] = make(map[[32]byte]map[spectypes.OperatorID]spectypes.Signature, len(roots))
		for rootHex, ops := range roots {
			rootBytes, err := hex.DecodeString(rootHex)
			if err != nil {
				return fmt.Errorf("decode signing root: %w", err)
			}
			if len(rootBytes) != 32 {
				return fmt.Errorf("invalid signing root length: %d", len(rootBytes))
			}
			var root [32]byte
			copy(root[:], rootBytes)
			ps.Signatures[vi][root] = ops
		}
	}

	return nil
}
