package validation

// signer_state.go describes state of a signer.

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// SignerState represents the state of a signer, including its start time, slot, round,
// message counts, proposal data, and the number of duties performed in the current epoch.
type SignerState struct {
	Slot               phase0.Slot
	Round              specqbft.Round
	MessageCounts      MessageCounts
	ProposalData       []byte
	EpochDuties        int
	SeenDecidedLengths map[int]struct{}
}

// ResetSlot resets the state's slot, round, message counts, and proposal data to the given values.
// It also updates the start time to the current time and increments the epoch duties count if it's a new epoch.
func (s *SignerState) ResetSlot(slot phase0.Slot, round specqbft.Round, newEpoch bool) {
	s.Slot = slot
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
	if newEpoch {
		s.EpochDuties = 1
	} else {
		s.EpochDuties++
	}
	s.SeenDecidedLengths = make(map[int]struct{})
}

// ResetRound resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerState) ResetRound(round specqbft.Round) {
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
	s.SeenDecidedLengths = make(map[int]struct{})
}
