// Package twoab is the SSV-side adapter that bridges the 2abOBFT consensus
// core (protocol/v2/obft/twoab) with SSV's runtime — concrete types from
// ssv-spec, the network layer, the runner lifecycle. It is the 2abOBFT
// sibling of the bare-OBFT adapter in protocol/v2/ssv/runner/obft, driving
// twoab.Instance instead of base.Instance.
package twoab

import (
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// 2abOBFT proposer-duty candidate encoding: V on the wire is
//
//	[1 byte version | SSZ-marshaled blinded BeaconBlock]
//
// Identical to the bare-OBFT encoding (the SSV proposer-duty V convention):
// the block (not its signing root) travels in V so peers can host-validate
// against the full payload. The proposer signer translates V → signing root
// before signing/verifying — see proposer_signer.go.

// EncodeCandidate packages a blinded block + its spec version for 2abOBFT
// agreement.
func EncodeCandidate(version spec.DataVersion, blindedSSZ []byte) []byte {
	out := make([]byte, 1+len(blindedSSZ))
	out[0] = byte(version)
	copy(out[1:], blindedSSZ)
	return out
}

// DecodeCandidate splits the encoded V into (version, blinded SSZ).
func DecodeCandidate(value []byte) (spec.DataVersion, []byte, error) {
	if len(value) < 1 {
		return spec.DataVersionUnknown, nil, errors.New("twoab candidate: empty value")
	}
	return spec.DataVersion(value[0]), value[1:], nil
}

// DecodeBlindedProposal decodes a blinded SSZ block into a VersionedProposal
// for the given spec version.
func DecodeBlindedProposal(version spec.DataVersion, blindedSSZ []byte) (*api.VersionedProposal, error) {
	cd := &spectypes.ValidatorConsensusData{
		Version: version,
		DataSSZ: blindedSSZ,
	}
	vBlk, _, err := cd.GetBlockData()
	if err != nil {
		return nil, fmt.Errorf("twoab candidate: decode blinded proposal: %w", err)
	}
	return vBlk, nil
}
