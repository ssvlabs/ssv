package ethtest

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
)

type testClusterReactivatedInput struct {
	*CommonTestInput
	events []*ClusterReactivatedEventInput
}

func NewTestClusterReactivatedInput(common *CommonTestInput) *testClusterReactivatedInput {
	return &testClusterReactivatedInput{common, nil}
}

func (input *testClusterReactivatedInput) validate() error {
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

type ClusterReactivatedEventInput struct {
	auth    *bind.TransactOpts
	opsIds  []uint64
	cluster *simcontract.CallableCluster
}

func (input *ClusterReactivatedEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.cluster == nil:
		return fmt.Errorf("validation error: input.cluster is empty")
	case len(input.opsIds) == 0:
		return fmt.Errorf("validation error: input.opsIds is empty")
	}

	return nil
}

func (input *testClusterReactivatedInput) prepare(
	eventsToDo []*ClusterReactivatedEventInput,
) {
	input.events = eventsToDo
}

func (input *testClusterReactivatedInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		// Call the contract method
		_, err = input.boundContract.Reactivate(
			event.auth,
			event.opsIds,
			big.NewInt(100_000_000),
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
