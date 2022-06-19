package qbft

import "github.com/pkg/errors"

type Timer interface {
	// TimeoutForRound will reset running timer if exists and will start a new timer for a specific round
	TimeoutForRound(round Round)
}

func uponRoundTimeout(state *State, config IConfig) error {
	state.Round++
	roundChange := createRoundChange(state, state.Round)

	if err := config.GetNetwork().Broadcast(roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}
