package validation

// signer_state.go describes state of a signer.

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

type SignerState struct {
	Start         time.Time
	Slot          phase0.Slot
	Round         specqbft.Round
	MessageCounts MessageCounts
	ProposalData  []byte
	EpochDuties   int
}

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

func (s *SignerState) ResetRound(round specqbft.Round) {
	s.Start = time.Now()
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
}
