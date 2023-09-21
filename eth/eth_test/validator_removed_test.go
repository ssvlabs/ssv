package eth_test

import (
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

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
