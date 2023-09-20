package eth_test

import (
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/simulator"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

type commonTestInput struct {
	sim           *simulator.SimulatedBackend
	boundContract *simcontract.Simcontract
	blockNum      *uint64
	nodeStorage   storage.Storage
	doInOneBlock  bool
}

type testOperatorAddedEventInput struct {
	op   *testOperator
	auth *bind.TransactOpts
}

type produceOperatorAddedEventsInput struct {
	*commonTestInput
	events []*testOperatorAddedEventInput
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

func produceValidatorRegisteredEvents(
	t *testing.T,
	input *produceValidatorRegisteredEventsInput,
) {
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

type testValidatorRemovedInput struct {
	auth      *bind.TransactOpts
	ops       []*testOperator
	validator *testValidatorData
	share     []byte
	opsIds    []uint64 // separating opsIds from ops as it is a separate event field and should be used for destructive tests
}

type produceValidatorRemovedEventsInput struct {
	*commonTestInput
	events []*testValidatorRemovedInput
}

func produceValidatorRemovedEvents(
	t *testing.T,
	input *produceValidatorRemovedEventsInput,
) {
	for _, event := range input.events {
		val := event.validator
		valPubKey := val.masterPubKey.Serialize()
		// Check the validator's shares are present in the state before removing
		valShare := input.nodeStorage.Shares().Get(nil, valPubKey)
		require.NotNil(t, valShare)

		_, err := input.boundContract.SimcontractTransactor.RemoveValidator(
			event.auth,
			val.masterPubKey.Serialize(),
			event.opsIds,
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           2,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
		require.NoError(t, err)
		input.sim.Commit()
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
