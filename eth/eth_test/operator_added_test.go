package eth_test

import (
	"fmt"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
	"math/big"
)

type testOperatorAddedEventInput struct {
	op   *testOperator
	auth *bind.TransactOpts
}

type ProduceOperatorAddedEventsInput struct {
	*commonTestInput
	events []*testOperatorAddedEventInput
}

func NewOperatorAddedEventInput(common *commonTestInput) *ProduceOperatorAddedEventsInput {
	return &ProduceOperatorAddedEventsInput{common, nil}
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

func (input *ProduceOperatorAddedEventsInput) prepare(
	ops []*testOperator,
	auth *bind.TransactOpts,
) {
	input.events = make([]*testOperatorAddedEventInput, len(ops))

	for i, op := range ops {
		input.events[i] = &testOperatorAddedEventInput{op, auth}
	}
}

func (input *ProduceOperatorAddedEventsInput) produce() {
	for _, event := range input.events {
		op := event.op
		packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op.pub)
		require.NoError(input.t, err)
		_, err = input.boundContract.SimcontractTransactor.RegisterOperator(event.auth, packedOperatorPubKey, big.NewInt(100_000_000))
		require.NoError(input.t, err)

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
