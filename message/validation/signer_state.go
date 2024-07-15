package validation

// signer_state.go describes state of a signer.

import (
	"crypto/sha256"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// SignerState represents the state of a signer, including its start time, slot, round,
// message counts, proposal data, and the number of duties performed in the current epoch.
type SignerState struct {
	Slot          phase0.Slot // index stores slot modulo, so we also need to store slot here
	Round         specqbft.Round
	MessageCounts MessageCounts
	ProposalData  []byte
	SeenSigners   map[[sha256.Size]byte]struct{}
}

func NewSignerState(slot phase0.Slot, round specqbft.Round) *SignerState {
	s := &SignerState{}
	s.Reset(slot, round)
	return s
}

// Reset resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerState) Reset(slot phase0.Slot, round specqbft.Round) {
	s.Slot = slot
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
	s.SeenSigners = make(map[[sha256.Size]byte]struct{})
}
