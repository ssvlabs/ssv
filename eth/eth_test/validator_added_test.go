package eth_test

import (
	"fmt"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/bloxapp/ssv/operator/storage"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

type testValidatorRegisteredEventInput struct {
	auth      *bind.TransactOpts
	ops       []*testOperator
	validator *testValidatorData
	share     []byte
	opsIds    []uint64 // separating opsIds from ops as it is a separate event field and should be used for destructive tests
}

type produceValidatorRegisteredEventsInput struct {
	*commonTestInput
	events []*testValidatorRegisteredEventInput
}

func (input *produceValidatorRegisteredEventsInput) validate() error {
	for _, e := range input.events {
		if err := e.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (input *testValidatorRegisteredEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}
	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.validator == nil:
		return fmt.Errorf("validation error: input.validator is empty")
	case len(input.share) == 0:
		return fmt.Errorf("validation error: input.validator is empty")
	case len(input.ops) == 0:
		return fmt.Errorf("validation error: input.ops is empty")
	}

	if len(input.opsIds) == 0 {
		input.opsIds = make([]uint64, len(input.ops))
		for i, op := range input.ops {
			input.opsIds[i] = op.id
		}
	}

	return nil
}

func prepareValidatorAddedEvents(
	t *testing.T,
	nodeStorage storage.Storage,
	validators []*testValidatorData,
	shares [][]byte,
	ops []*testOperator,
	auth *bind.TransactOpts,
	expectedNonce *registrystorage.Nonce,
	validatorsIds []uint32,
) []*testValidatorRegisteredEventInput {
	events := make([]*testValidatorRegisteredEventInput, len(validatorsIds))

	for i, validatorId := range validatorsIds {
		// Check there are no shares in the state for the current validator
		valPubKey := validators[validatorId].masterPubKey.Serialize()
		share := nodeStorage.Shares().Get(nil, valPubKey)
		require.Nil(t, share)

		// Create event input
		events[i] = &testValidatorRegisteredEventInput{
			validator: validators[validatorId],
			share:     shares[validatorId],
			auth:      auth,
			ops:       ops,
		}

		// expect nonce bumping after each of these ValidatorAdded events handling
		*expectedNonce++
	}
	return events
}

func produceValidatorRegisteredEvents(
	t *testing.T,
	input *produceValidatorRegisteredEventsInput,
) {
	err := input.validate()
	require.NoError(t, err)

	for _, event := range input.events {
		val := event.validator
		valPubKey := val.masterPubKey.Serialize()
		shares := input.nodeStorage.Shares().Get(nil, valPubKey)
		require.Nil(t, shares)

		// Call the contract method
		_, err := input.boundContract.SimcontractTransactor.RegisterValidator(
			event.auth,
			val.masterPubKey.Serialize(),
			event.opsIds,
			event.share,
			big.NewInt(100_000_000),
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           1,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
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
