package ethtest

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
)

type testOperatorRemovedEventInput struct {
	opId uint64
	auth *bind.TransactOpts
}

type ProduceOperatorRemovedEventsInput struct {
	*CommonTestInput
	events []*testOperatorRemovedEventInput
}

func NewOperatorRemovedEventInput(common *CommonTestInput) *ProduceOperatorRemovedEventsInput {
	return &ProduceOperatorRemovedEventsInput{common, nil}
}

func (input *ProduceOperatorRemovedEventsInput) validate() error {
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
func (input *testOperatorRemovedEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.opId == 0:
		return fmt.Errorf("validation error: input.opId is invalid")
	}

	return nil
}

func (input *ProduceOperatorRemovedEventsInput) prepare(
	opsIds []uint64,
	auth *bind.TransactOpts,
) {
	input.events = make([]*testOperatorRemovedEventInput, len(opsIds))

	for i, opId := range opsIds {
		input.events[i] = &testOperatorRemovedEventInput{opId, auth}
	}
}

func (input *ProduceOperatorRemovedEventsInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		_, err = input.boundContract.RemoveOperator(
			event.auth,
			event.opId,
		)
		require.NoError(input.t, err)

		if !input.doInOneBlock {
			commitBlock(input.sim, input.blockNum)
		}
	}
	if input.doInOneBlock {
		commitBlock(input.sim, input.blockNum)
	}
}
