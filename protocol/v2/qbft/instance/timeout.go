package instance

import (
	"fmt"
	"github.com/pkg/errors"
)

func (i *Instance) UponRoundTimeout() error {
	newRound := i.State.Round + 1
	fmt.Println("UponRoundTimeout:", newRound)
	i.State.Round = newRound
	i.State.ProposalAcceptedForCurrentRound = nil
	i.config.GetTimer().TimeoutForRound(i.State.Round)

	roundChange, err := CreateRoundChange(i.State, i.config, newRound, i.StartValue)
	if err != nil {
		return errors.Wrap(err, "could not generate round change msg")
	}

	if err := i.Broadcast(roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}
