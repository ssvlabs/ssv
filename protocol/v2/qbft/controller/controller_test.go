package controller

import (
	"context"
	"crypto/rsa"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/observability/log"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// testingNetwork wraps the spec testing network to satisfy protocolp2p.Network. Defined
// locally (rather than reusing protocol/v2/testing.TestingNetwork) to avoid an import cycle:
// protocol/v2/testing imports this package (protocol/v2/qbft/controller).
type testingNetwork struct {
	*spectestingutils.TestingNetwork
}

func newTestingNetwork(operatorID spectypes.OperatorID, sk *rsa.PrivateKey) *testingNetwork {
	return &testingNetwork{TestingNetwork: spectestingutils.NewTestingNetwork(operatorID, sk)}
}

func (n *testingNetwork) Subscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *testingNetwork) Unsubscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *testingNetwork) BroadcastAtSlot(message *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	return n.Broadcast(message.SSVMessage.GetID(), message)
}

func (n *testingNetwork) ReportValidation(_ *spectypes.SSVMessage, _ protocolp2p.MsgValidationResult) {
}

func TestController_Marshaling(t *testing.T) {
	c := qbft.TestingControllerStruct

	byts, err := c.Encode()
	require.NoError(t, err)

	decoded := &Controller{
		// Instances is a concrete slice type with custom UnmarshalJSON — we must pre-size it so
		// unmarshaling populates it correctly (the capacity bounds how many instances are kept).
		RecentInstances: make(Instances, 0, InstancesTestCapacity),
	}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}

func TestController_OnQBFTRoundTimeoutWithRoundCheck(t *testing.T) {
	// Initialize logger
	logger := log.TestLogger(t)

	keySet := spectestingutils.Testing4SharesSet()
	testConfig := &qbft.Config{
		BeaconSigner: ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
		Network:      newTestingNetwork(1, keySet.OperatorKeys[1]),
		CutOffRound:  spectestingutils.TestingCutOffRound,
	}

	identifier := make([]byte, 56)
	identifier[0] = 1
	identifier[1] = 2
	identifier[2] = 3
	identifier[3] = 4

	share := spectestingutils.TestingCommitteeMember(keySet)
	inst := instance.NewInstance(
		t.Context(),
		logger,
		testConfig,
		share,
		identifier,
		specqbft.FirstHeight,
		spectestingutils.TestingOperatorSigner(keySet),
		func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
			return roundtimer.NewTestingTimer()
		},
	)

	// Initialize Controller
	contr := &Controller{}

	// Initialize EventMsg for the test
	timeoutData := &types.TimeoutData{
		Slot:  0,
		Round: specqbft.FirstRound,
	}

	// Simulate a scenario where the instance is at a higher round
	inst.State.Round = specqbft.Round(2)
	contr.RecentInstances.addNewInstance(inst)

	// Call OnQBFTRoundTimeout and capture the error
	err := contr.OnQBFTRoundTimeout(context.TODO(), logger, timeoutData)

	// Assert that the error is nil and the round did not bump
	require.NoError(t, err)
	require.Equal(t, specqbft.Round(2), inst.State.Round, "Round should not bump")

	// Simulate a scenario where the instance is at the same or lower round
	inst.State.Round = specqbft.FirstRound

	// Call OnQBFTRoundTimeout and capture the error
	err = contr.OnQBFTRoundTimeout(context.TODO(), logger, timeoutData)

	// Assert that the error is nil and the round did bump
	require.NoError(t, err)
	require.Equal(t, specqbft.Round(2), inst.State.Round, "Round should bump")
}
