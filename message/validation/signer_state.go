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
}

func (s *SignerState) Reset(slot phase0.Slot, round specqbft.Round) {
	s.Start = time.Now()
	s.Slot = slot
	s.Round = round
	s.MessageCounts = MessageCounts{}
}
