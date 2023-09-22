package eth_test

import (
	"fmt"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
)

type testValidatorRemovedInput struct {
	auth      *bind.TransactOpts
	validator *testValidatorData
	opsIds    []uint64
	cluster   *simcontract.CallableCluster
}

func (input *testValidatorRemovedInput) validate() error {
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

type testValidatorRemovedEventsInput struct {
	*commonTestInput
	events []*testValidatorRemovedInput
}

func (input *testValidatorRemovedEventsInput) validate() error {
	for _, e := range input.events {
		if err := e.validate(); err != nil {
			return err
		}
	}
	return nil
}

func NewTestValidatorRemovedEventsInput(common *commonTestInput) *testValidatorRemovedEventsInput {
	return &testValidatorRemovedEventsInput{common, nil}
}

func (input *testValidatorRemovedEventsInput) prepare(
	validators []*testValidatorData,
	validatorsIds []uint64,
	opsIds []uint64,
	auth *bind.TransactOpts,
	cluster *simcontract.CallableCluster,
) {
	input.events = make([]*testValidatorRemovedInput, len(validatorsIds))

	for i, validatorId := range validatorsIds {
		input.events[i] = &testValidatorRemovedInput{
			auth,
			validators[validatorId],
			opsIds,
			cluster,
		}
	}
}

func (input *testValidatorRemovedEventsInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		valPubKey := event.validator.masterPubKey.Serialize()
		// Check the validator's shares are present in the state before removing
		valShare := input.nodeStorage.Shares().Get(nil, valPubKey)
		require.NotNil(input.t, valShare)

		_, err = input.boundContract.SimcontractTransactor.RemoveValidator(
			event.auth,
			valPubKey,
			event.opsIds,
			*event.cluster,
		)
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
