package ethtest

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
)

type testValidatorExitedInput struct {
	auth      *bind.TransactOpts
	validator *testValidatorData
	opsIds    []uint64
	cluster   *simcontract.CallableCluster
}

func (input *testValidatorExitedInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.validator == nil:
		return fmt.Errorf("validation error: input.validator is empty")
	case len(input.opsIds) == 0:
		return fmt.Errorf("validation error: input.opsIds is empty")
	}

	return nil
}

type TestValidatorExitedEventsInput struct {
	*CommonTestInput
	events []*testValidatorExitedInput
}

func (input *TestValidatorExitedEventsInput) validate() error {
	if input.CommonTestInput == nil {
		return fmt.Errorf("validation error: empty CommonTestInput")
	}
	if input.events == nil {
		return fmt.Errorf("validation error: empty events")
	}
	for _, e := range input.events {
		if err := e.validate(); err != nil {
			return err
		}
	}
	return nil
}

func NewTestValidatorExitedEventsInput(common *CommonTestInput) *TestValidatorExitedEventsInput {
	return &TestValidatorExitedEventsInput{common, nil}
}

func (input *TestValidatorExitedEventsInput) prepare(
	validators []*testValidatorData,
	validatorsIds []uint64,
	opsIds []uint64,
	auth *bind.TransactOpts,
	cluster *simcontract.CallableCluster,
) {
	input.events = make([]*testValidatorExitedInput, len(validatorsIds))

	for i, validatorId := range validatorsIds {
		input.events[i] = &testValidatorExitedInput{
			auth,
			validators[validatorId],
			opsIds,
			cluster,
		}
	}
}

func (input *TestValidatorExitedEventsInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		valPubKey := event.validator.masterPubKey.Serialize()
		// Check the validator's shares are present in the state before exiting
		valShare, exists := input.nodeStorage.Shares().Get(nil, valPubKey)
		require.True(input.t, exists)
		require.NotNil(input.t, valShare)

		_, err = input.boundContract.ExitValidator(
			event.auth,
			valPubKey,
			event.opsIds,
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
