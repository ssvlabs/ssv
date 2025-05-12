package ethtest

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/eth/eventparser"
)

type testOperatorAddedEventInput struct {
	op   *testOperator
	auth *bind.TransactOpts
}

type ProduceOperatorAddedEventsInput struct {
	*CommonTestInput
	events []*testOperatorAddedEventInput
}

func NewOperatorAddedEventInput(common *CommonTestInput) *ProduceOperatorAddedEventsInput {
	return &ProduceOperatorAddedEventsInput{common, nil}
}

func (input *ProduceOperatorAddedEventsInput) validate() error {
	if input.CommonTestInput == nil {
		return fmt.Errorf("validation error: CommonTestInput is empty")
	}
	if input.events == nil {
		return fmt.Errorf("validation error: empty events")
	}
	for _, event := range input.events {
		err := event.validate()
		if err != nil {
			return err
		}
	}
	return nil
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
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		op := event.op
		encodedPubKey, err := op.privateKey.Public().Base64()
		require.NoError(input.t, err)

		packedOperatorPubKey, err := eventparser.PackOperatorPublicKey([]byte(encodedPubKey))
		require.NoError(input.t, err)

		_, err = input.boundContract.RegisterOperator(event.auth, packedOperatorPubKey, big.NewInt(100_000_000))
		require.NoError(input.t, err)

		if !input.doInOneBlock {
			commitBlock(input.sim, input.blockNum)
		}
	}
	if input.doInOneBlock {
		commitBlock(input.sim, input.blockNum)
	}
}
