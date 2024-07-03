package validation

// signer_state.go describes state of a signer.

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// SignerState represents the state of a signer, including its start time, slot, round,
// message counts, proposal data, and the number of duties performed in the current epoch.
type SignerState struct {
	Round         specqbft.Round
	MessageCounts MessageCounts
	ProposalData  []byte
	SeenSigners   map[string]struct{}
}

func NewSignerState() *SignerState {
	return &SignerState{
		Round:         specqbft.FirstRound,
		SeenSigners:   make(map[string]struct{}),
		MessageCounts: MessageCounts{},
		ProposalData:  []byte{},
	}
}

// ResetRound resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerState) ResetRound(round specqbft.Round) {
	s.Round = round
	s.MessageCounts = MessageCounts{}
	s.ProposalData = nil
	s.SeenSigners = make(map[string]struct{})
}
