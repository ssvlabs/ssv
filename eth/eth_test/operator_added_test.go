package eth_test

import (
	"fmt"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

type testOperatorAddedEventInput struct {
	op   *testOperator
	auth *bind.TransactOpts
}

type produceOperatorAddedEventsInput struct {
	*commonTestInput
	events []*testOperatorAddedEventInput
}

func (input *testOperatorAddedEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.op == nil:
		return fmt.Errorf("validation error: input.op is empty")
	}

	return nil
}

func prepareOperatorAddedEvents(
	ops []*testOperator,
	auth *bind.TransactOpts,
) []*testOperatorAddedEventInput {
	events := make([]*testOperatorAddedEventInput, len(ops))

	for i, op := range ops {
		events[i] = &testOperatorAddedEventInput{op, auth}
	}

	return events
}

func produceOperatorAddedEvents(
	t *testing.T,
	input *produceOperatorAddedEventsInput,
) {
	for _, event := range input.events {
		op := event.op
		packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op.pub)
		require.NoError(t, err)
		_, err = input.boundContract.SimcontractTransactor.RegisterOperator(event.auth, packedOperatorPubKey, big.NewInt(100_000_000))
		require.NoError(t, err)

		if !input.doInOneBlock {
			input.sim.Commit()
			*input.blockNum++
		}
	}
	if input.doInOneBlock {
		input.sim.Commit()
		*input.blockNum++
	}
}
