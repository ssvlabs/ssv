package validator

import (
	"fmt"

	"github.com/bloxapp/ssv/eth1/abiparser"
)

const maxOperators = 13

func validateValidatorAddedEvent(event abiparser.ValidatorAddedEvent) error {
	operatorCount := len(event.OperatorIds)

	if operatorCount > maxOperators {
		return fmt.Errorf("too many operators (%d)", operatorCount)
	}

	if operatorCount == 0 {
		return fmt.Errorf("no operators")
	}

	canBeQuorum := func(v int) bool {
		return (v-1)%3 == 0 && (v-1)/3 != 0
	}

	if !canBeQuorum(len(event.OperatorIds)) {
		return fmt.Errorf("given operator count (%d) cannot build a 3f+1 quorum", operatorCount)
	}

	seenOperators := map[uint64]struct{}{}
	for _, operatorID := range event.OperatorIds {
		if _, ok := seenOperators[operatorID]; ok {
			return fmt.Errorf("duplicated operator ID (%d)", operatorID)
		}
		seenOperators[operatorID] = struct{}{}
	}

	return nil
}
