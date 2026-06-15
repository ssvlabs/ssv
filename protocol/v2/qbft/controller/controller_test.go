package controller

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

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
		Network:      spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
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

// TestIsRound1Leader checks that IsRound1Leader returns the correct value for a known
// committee and set of QBFT heights.  The committee has 4 operators with IDs 1-4 sorted
// ascending, so RoundRobinProposer picks Committee[height%4] for Round 1.
func TestIsRound1Leader(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	baseMember := spectestingutils.TestingCommitteeMember(keySet)

	testConfig := &qbft.Config{
		BeaconSigner: ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
		Network:      spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
		CutOffRound:  spectestingutils.TestingCutOffRound,
	}
	identifier := baseMember.CommitteeID[:]

	tests := []struct {
		name       string
		operatorID spectypes.OperatorID
		height     specqbft.Height
		wantLeader bool
	}{
		// height 12: 12%4==0 → Committee[0] = operator 1
		{name: "op1 leads height12", operatorID: spectypes.OperatorID(1), height: 12, wantLeader: true},
		{name: "op2 not lead height12", operatorID: spectypes.OperatorID(2), height: 12, wantLeader: false},
		// height 13: 13%4==1 → Committee[1] = operator 2
		{name: "op1 not lead height13", operatorID: spectypes.OperatorID(1), height: 13, wantLeader: false},
		{name: "op2 leads height13", operatorID: spectypes.OperatorID(2), height: 13, wantLeader: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Construct a CommitteeMember with the test operator ID but the same
			// committee roster so the round-robin index is deterministic.
			member := *baseMember
			member.OperatorID = tt.operatorID
			ctrl := NewController(identifier, &member, testConfig, nil, false)

			require.Equal(t, tt.wantLeader, ctrl.IsRound1Leader(tt.height))
		})
	}
}
