package ethtest

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type testValidatorRegisteredInput struct {
	*CommonTestInput
	events []*validatorRegisteredEventInput
}

func NewTestValidatorRegisteredInput(common *CommonTestInput) *testValidatorRegisteredInput {
	return &testValidatorRegisteredInput{common, nil}
}

func (input *testValidatorRegisteredInput) validate() error {
	if input.CommonTestInput == nil {
		return fmt.Errorf("validation error: CommonTestInput is empty")
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

type validatorRegisteredEventInput struct {
	auth      *bind.TransactOpts
	ops       []*testOperator
	validator *testValidatorData
	share     []byte
	opsIds    []uint64 // separating opsIds from ops as it is a separate event field and should be used for destructive tests
}

func (input *validatorRegisteredEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.validator == nil:
		return fmt.Errorf("validation error: input.validator is empty")
	case len(input.share) == 0:
		return fmt.Errorf("validation error: input.share is empty")
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

func (input *testValidatorRegisteredInput) prepare(
	validators []*testValidatorData,
	shares [][]byte,
	ops []*testOperator,
	auth *bind.TransactOpts,
	expectedNonce *registrystorage.Nonce,
	validatorsIds []uint32,
) {
	input.events = make([]*validatorRegisteredEventInput, len(validatorsIds))

	for i, validatorId := range validatorsIds {
		// Check there are no shares in the state for the current validator
		valPubKey := validators[validatorId].masterPubKey.Serialize()
		share, exists := input.nodeStorage.Shares().Get(nil, valPubKey)
		require.False(input.t, exists)
		require.Nil(input.t, share)

		// Create event input
		input.events[i] = &validatorRegisteredEventInput{
			validator: validators[validatorId],
			share:     shares[validatorId],
			auth:      auth,
			ops:       ops,
		}

		// expect nonce bumping after each of these ValidatorAdded events handling
		*expectedNonce++
	}
}

func (input *testValidatorRegisteredInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		val := event.validator
		valPubKey := val.masterPubKey.Serialize()
		shares, exists := input.nodeStorage.Shares().Get(nil, valPubKey)
		require.False(input.t, exists)
		require.Nil(input.t, shares)

		// Call the contract method
		_, err := input.boundContract.RegisterValidator(
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
		require.NoError(input.t, err)

		if !input.doInOneBlock {
			commitBlock(input.sim, input.blockNum)
		}
	}
	if input.doInOneBlock {
		commitBlock(input.sim, input.blockNum)
	}
}
