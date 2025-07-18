package controller

import (
	"context"
	"encoding/json"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
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
		BeaconSigner: ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
		Network:      spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
		Timer:        roundtimer.NewTestingTimer(),
		CutOffRound:  spectestingutils.TestingCutOffRound,
	}

	identifier := make([]byte, 56)
	identifier[0] = 1
	identifier[1] = 2
	identifier[2] = 3
	identifier[3] = 4

	share := spectestingutils.TestingCommitteeMember(keySet)
	inst := instance.NewInstance(
		testConfig,
		share,
		identifier,
		specqbft.FirstHeight,
		spectestingutils.TestingOperatorSigner(keySet),
	)

	// Initialize Controller
	contr := &Controller{}

	// Initialize EventMsg for the test
	timeoutData := types.TimeoutData{
		Height: specqbft.FirstHeight,
		Round:  specqbft.FirstRound,
	}

	data, err := json.Marshal(timeoutData)
	require.NoError(t, err)

	msg := &types.EventMsg{
		Type: types.Timeout,
		Data: data,
	}

	// Simulate a scenario where the instance is at a higher round
	inst.State.Round = specqbft.Round(2)
	contr.StoredInstances.addNewInstance(inst)

	// Call OnTimeout and capture the error
	err = contr.OnTimeout(context.TODO(), logger, *msg)

	// Assert that the error is nil and the round did not bump
	require.NoError(t, err)
	require.Equal(t, specqbft.Round(2), inst.State.Round, "Round should not bump")

	// Simulate a scenario where the instance is at the same or lower round
	inst.State.Round = specqbft.FirstRound

	// Call OnTimeout and capture the error
	err = contr.OnTimeout(context.TODO(), logger, *msg)

	// Assert that the error is nil and the round did bump
	require.NoError(t, err)
	require.Equal(t, specqbft.Round(2), inst.State.Round, "Round should bump")
}
