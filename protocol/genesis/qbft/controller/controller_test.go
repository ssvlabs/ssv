package controller

import (
	"encoding/json"
	"testing"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
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

	testConfig := &qbft.Config{
		Signer:  spectestingutils.NewTestingKeyManager(),
		Network: spectestingutils.NewTestingNetwork(),
		Timer:   roundtimer.NewTestingTimer(),
	}

	share := spectestingutils.TestingShare(spectestingutils.Testing4SharesSet())
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
		Height: genesisspecqbft.FirstHeight,
		Round:  genesisspecqbft.FirstRound,
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
