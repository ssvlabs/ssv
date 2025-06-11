package ethtest

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
)

type testClusterLiquidatedInput struct {
	*CommonTestInput
	events []*ClusterLiquidatedEventInput
}

func NewTestClusterLiquidatedInput(common *CommonTestInput) *testClusterLiquidatedInput {
	return &testClusterLiquidatedInput{common, nil}
}

func (input *testClusterLiquidatedInput) validate() error {
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

type ClusterLiquidatedEventInput struct {
	auth         *bind.TransactOpts
	ownerAddress *ethcommon.Address
	opsIds       []uint64
	cluster      *simcontract.CallableCluster
}

func (input *ClusterLiquidatedEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.ownerAddress == nil:
		return fmt.Errorf("validation error: input.ownerAddress is empty")
	case input.cluster == nil:
		return fmt.Errorf("validation error: input.cluster is empty")
	case len(input.opsIds) == 0:
		return fmt.Errorf("validation error: input.opsIds is empty")
	}

	return nil
}

func (input *testClusterLiquidatedInput) prepare(
	eventsToDo []*ClusterLiquidatedEventInput,
) {
	input.events = eventsToDo
}

func (input *testClusterLiquidatedInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {

		// Call the contract method
		_, err = input.boundContract.Liquidate(
			event.auth,
			*event.ownerAddress,
			event.opsIds,
			*event.cluster,
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
