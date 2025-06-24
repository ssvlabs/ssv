package validation

// signer_state.go describes state of a signer.

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// SignerState represents the state of a signer, including its start time, slot, round,
// message counts, proposal data, and the number of duties performed in the current epoch.
type SignerState struct {
	Slot         phase0.Slot // index stores slot modulo, so we also need to store slot here
	Round        specqbft.Round
	SeenMsgTypes SeenMsgTypes
	// Storing pointer to byte array instead of slice to reduce memory consumption when we don't need the hash.
	// A nil slice could be an alternative, but it'd consume more memory, and we'd need to cast [32]byte returned by sha256.Sum256() to slice.
	HashedProposalData *[32]byte
	// Max possible map size for committee sizes:
	//  4 (f=1): C(4,3)+C(4,4)=5
	//  7 (f=2): C(7,5)+C(7,6)+C(7,7)=29
	// 10 (f=3): C(10,7)+C(10,8)+C(10,9)+C(10,10)=176
	// 13 (f=4): C(13,9)+C(13,10)+C(13,11)+C(13,12)+C(13,13)=1093
	SeenSigners map[SignersBitMask]struct{}
}

func newSignerState(slot phase0.Slot, round specqbft.Round) *SignerState {
	s := &SignerState{}
	s.Reset(slot, round)
	return s
}

// Reset resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerState) Reset(slot phase0.Slot, round specqbft.Round) {
	s.Slot = slot
	s.Round = round
	s.SeenMsgTypes = SeenMsgTypes{}
	s.HashedProposalData = nil
	s.SeenSigners = nil // lazy init on demand to reduce mem consumption
}
