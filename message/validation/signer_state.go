package validation

// signer_state.go describes state of a signer.

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

// SignerState represents the state of a signer, including its start time, slot, round,
// message counts, proposal data, and the number of duties performed in the current epoch.
type SignerState struct {
	Start         time.Time
	Slot          phase0.Slot
	Round         specqbft.Round
	MessageCounts MessageCounts
	ProposalData  []byte
	EpochDuties   int
}

// ResetSlot resets the state's slot, round, message counts, and proposal data to the given values.
// It also updates the start time to the current time and increments the epoch duties count if it's a new epoch.
func (s *SignerState) ResetSlot(slot phase0.Slot, round specqbft.Round, newEpoch bool) {
	s.Start = time.Now()
	s.Slot = slot
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
	if newEpoch {
		s.EpochDuties = 1
	} else {
		s.EpochDuties++
	}
}

// ResetRound resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerState) ResetRound(round specqbft.Round) {
	s.Start = time.Now()
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
}
