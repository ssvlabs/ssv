package controller

import (
	"encoding/json"
	"testing"

	"github.com/bloxapp/ssv/logging"
	qbft "github.com/bloxapp/ssv/protocol/v2/genesisqbft"
	"github.com/bloxapp/ssv/protocol/v2/genesisqbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/genesisqbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/stretchr/testify/require"

	spectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
)

func TestController_Marshaling(t *testing.T) {
	c := qbft.TestingControllerStruct

	byts, err := c.Encode()
	require.NoError(t, err)

	decoded := &Controller{
		// Since StoredInstances is an interface, it wouldn't be decoded properly.
		// Therefore, we set it to NewInstanceContainer which implements json.Unmarshaler
		StoredInstances: make(InstanceContainer, 0, InstanceContainerTestCapacity),
	}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}

func TestController_OnTimeoutWithRoundCheck(t *testing.T) {
	// Initialize logger
	logger := logging.TestLogger(t)

	keySet := spectestingutils.Testing4SharesSet()
	testConfig := &qbft.Config{
		ShareSigner:    spectestingutils.NewTestingKeyManager(),
		OperatorSigner: spectestingutils.NewTestingOperatorSigner(keySet, 1),
		Network:        spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
		Timer:          roundtimer.NewTestingTimer(),
	}

	share := spectestingutils.TestingShare(keySet)
	inst := instance.NewInstance(
		testConfig,
		share,
		[]byte{1, 2, 3, 4},
		genesisspecqbft.FirstHeight,
	)

	// Initialize Controller
	contr := &Controller{}

	// Initialize EventMsg for the test
	timeoutData := types.TimeoutData{
		Height: uint64(genesisspecqbft.FirstHeight),
		Round:  uint64(genesisspecqbft.FirstRound),
	}

	data, err := json.Marshal(timeoutData)
	require.NoError(t, err)

	msg := &types.EventMsg{
		Type: types.Timeout,
		Data: data,
	}

	// Simulate a scenario where the instance is at a higher round
	inst.State.Round = genesisspecqbft.Round(2)
	contr.StoredInstances.addNewInstance(inst)

	// Call OnTimeout and capture the error
	err = contr.OnTimeout(logger, *msg)

	// Assert that the error is nil and the round did not bump
	require.NoError(t, err)
	require.Equal(t, genesisspecqbft.Round(2), inst.State.Round, "Round should not bump")

	// Simulate a scenario where the instance is at the same or lower round
	inst.State.Round = genesisspecqbft.FirstRound

	// Call OnTimeout and capture the error
	err = contr.OnTimeout(logger, *msg)

	// Assert that the error is nil and the round did bump
	require.NoError(t, err)
	require.Equal(t, genesisspecqbft.Round(2), inst.State.Round, "Round should bump")
}
