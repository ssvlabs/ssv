package runner

import (
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/types"
)

type PartialSigContainer struct {
	Signatures map[phase0.ValidatorIndex]map[string]map[types.OperatorID][]byte
	// Quorum is the number of min signatures needed for quorum
	Quorum uint64
}

func NewPartialSigContainer(quorum uint64) *PartialSigContainer {
	return &PartialSigContainer{
		Quorum:     quorum,
		Signatures: make(map[phase0.ValidatorIndex]map[string]map[types.OperatorID][]byte),
	}
}

func (ps *PartialSigContainer) AddSignature(sigMsg *types.PartialSignatureMessage) {
	if ps.Signatures[sigMsg.ValidatorIndex] == nil {
		ps.Signatures[sigMsg.ValidatorIndex] = make(map[string]map[uint64][]byte)
	}
	if ps.Signatures[sigMsg.ValidatorIndex][rootHex(sigMsg.SigningRoot)] == nil {
		ps.Signatures[sigMsg.ValidatorIndex][rootHex(sigMsg.SigningRoot)] = make(map[types.OperatorID][]byte)
	}
	m := ps.Signatures[sigMsg.ValidatorIndex][rootHex(sigMsg.SigningRoot)]

	if m[sigMsg.Signer] == nil {
		m[sigMsg.Signer] = make([]byte, 96)
		copy(m[sigMsg.Signer], sigMsg.PartialSignature)
	}
}

// Returns if container has signature for signer and signing root
func (ps *PartialSigContainer) HasSigner(validatorIndex phase0.ValidatorIndex, signer types.OperatorID, signingRoot [32]byte) bool {
	if ps.Signatures[validatorIndex] == nil {
		return false
	}
	if ps.Signatures[validatorIndex][rootHex(signingRoot)] == nil {
		return false
	}
	return ps.Signatures[validatorIndex][rootHex(signingRoot)][signer] != nil
}

// Return signature for given root and signer
func (ps *PartialSigContainer) GetSignature(validatorIndex phase0.ValidatorIndex, signer types.OperatorID, signingRoot [32]byte) (types.Signature, error) {
	if ps.Signatures[validatorIndex] == nil {
		return nil, errors.New("Dont have signature for the given validator index")
	}
	if ps.Signatures[validatorIndex][rootHex(signingRoot)] == nil {
		return nil, errors.New("Dont have signature for the given signing root")
	}
	if ps.Signatures[validatorIndex][rootHex(signingRoot)][signer] == nil {
		return nil, errors.New("Dont have signature on signing root for the given signer")
	}
	return ps.Signatures[validatorIndex][rootHex(signingRoot)][signer], nil
}

// Return signature map for given root
func (ps *PartialSigContainer) GetSignatures(validatorIndex phase0.ValidatorIndex, signingRoot [32]byte) map[types.OperatorID][]byte {
	if ps.Signatures[validatorIndex] == nil {
		return nil
	}
	return ps.Signatures[validatorIndex][rootHex(signingRoot)]
}

// Remove signer from signature map
func (ps *PartialSigContainer) Remove(validatorIndex phase0.ValidatorIndex, signer uint64, signingRoot [32]byte) {
	if ps.Signatures[validatorIndex] == nil {
		return
	}
	if ps.Signatures[validatorIndex][rootHex(signingRoot)] == nil {
		return
	}
	if ps.Signatures[validatorIndex][rootHex(signingRoot)][signer] == nil {
		return
	}
	delete(ps.Signatures[validatorIndex][rootHex(signingRoot)], signer)
}

func (ps *PartialSigContainer) ReconstructSignature(root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	// Reconstruct signatures
	if ps.Signatures[validatorIndex] == nil {
		return nil, errors.New("no signatures for the given validator index")
	}
	if ps.Signatures[validatorIndex][rootHex(root)] == nil {
		return nil, errors.New("no signatures for the given signing root")
	}
	signature, err := types.ReconstructSignatures(ps.Signatures[validatorIndex][rootHex(root)])
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconstruct signatures")
	}

	// Get validator pub key copy (This avoids cgo Go pointer to Go pointer issue)
	validatorPubKeyCopy := make([]byte, len(validatorPubKey))
	copy(validatorPubKeyCopy, validatorPubKey)

	if err := types.VerifyReconstructedSignature(signature, validatorPubKeyCopy, root); err != nil {
		return nil, errors.Wrap(err, "failed to verify reconstruct signature")
	}
	return signature.Serialize(), nil
}

func (ps *PartialSigContainer) HasQuorum(validatorIndex phase0.ValidatorIndex, root [32]byte) bool {
	return uint64(len(ps.Signatures[validatorIndex][rootHex(root)])) >= ps.Quorum
}

func rootHex(r [32]byte) string {
	return hex.EncodeToString(r[:])
}
