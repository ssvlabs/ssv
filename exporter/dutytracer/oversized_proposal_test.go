package dutytracer

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/registry/storage"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

// TestOversizedValidatorProposalKeepsTraceEncodable checks the validator path: proposal data at the trace limit is
// archived, data one byte over is left out, and it doesn't replace what an earlier proposal stored. The trace
// encodes either way, so eviction can save it.
func TestOversizedValidatorProposalKeepsTraceEncodable(t *testing.T) {
	const (
		slot         = phase0.Slot(1)
		role, bnRole = ssvtypes.RoleAggregator, spectypes.BNRoleAggregator
		vIndex       = phase0.ValidatorIndex(55)
	)
	identifier := ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), role)

	proposal := func(t *testing.T, dataSSZ []byte) *queue.SSVMessage {
		msg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		data, err := (&specqbft.Message{MsgType: specqbft.ProposalMsgType}).Encode()
		require.NoError(t, err)
		msg.Data = data
		fullData, err := (&spectypes.ProposerConsensusData{DataSSZ: dataSSZ}).Encode()
		require.NoError(t, err)
		msg.SignedSSVMessage.FullData = fullData
		return msg
	}
	newCollector := func(t *testing.T) *Collector {
		ctrl := gomock.NewController(t)
		validators := registrystoragemocks.NewMockValidatorStore(ctrl)
		validators.EXPECT().ValidatorIndex(gomock.Any()).Return(vIndex, true).AnyTimes()
		return New(zap.NewNop(), validators, nil, nil, networkconfig.TestNetwork.Beacon, nil, nil)
	}
	archived := func(t *testing.T, c *Collector) *traces.ValidatorDutyTrace {
		duty, err := c.GetValidatorDuty(bnRole, slot, vIndex)
		require.NoError(t, err)
		_, err = duty.MarshalSSZ()
		require.NoError(t, err, "the trace must encode")
		return duty
	}

	atLimit := make([]byte, traces.MaxProposalDataSize)
	atLimit[0] = 1
	over := make([]byte, traces.MaxProposalDataSize+1)

	t.Run("at the limit", func(t *testing.T) {
		c := newCollector(t)
		require.NoError(t, c.Collect(t.Context(), proposal(t, atLimit), dummyVerify))
		require.Equal(t, atLimit, archived(t, c).ProposalData)
	})

	t.Run("one byte over", func(t *testing.T) {
		c := newCollector(t)
		require.NoError(t, c.Collect(t.Context(), proposal(t, over), dummyVerify))
		duty := archived(t, c)
		require.Empty(t, duty.ProposalData)
		require.Len(t, duty.Rounds, 1, "the rest of the trace is kept")
	})

	t.Run("one byte over after a proposal that fits", func(t *testing.T) {
		c := newCollector(t)
		require.NoError(t, c.Collect(t.Context(), proposal(t, atLimit), dummyVerify))
		require.NoError(t, c.Collect(t.Context(), proposal(t, over), dummyVerify))
		require.Equal(t, atLimit, archived(t, c).ProposalData)
	})
}

// TestOversizedCommitteeProposalKeepsTraceEncodable checks the same on the committee path, where the proposal's
// FullData goes into the trace as is.
func TestOversizedCommitteeProposalKeepsTraceEncodable(t *testing.T) {
	const slot = phase0.Slot(1)
	identifier := ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	proposal := func(fullData []byte) *queue.SSVMessage {
		msg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		msg.SignedSSVMessage.FullData = fullData
		return msg
	}
	newCollector := func(t *testing.T) *Collector {
		ctrl := gomock.NewController(t)
		validators := registrystoragemocks.NewMockValidatorStore(ctrl)
		validators.EXPECT().Committee(committeeID).
			Return(&storage.Committee{ID: committeeID, Operators: []spectypes.OperatorID{1, 2, 3, 4}}, true).AnyTimes()
		return New(zap.NewNop(), validators, mockclient{}, new(mockDutyTraceStore), networkconfig.TestNetwork.Beacon, nil, nil)
	}
	archived := func(t *testing.T, c *Collector) *traces.CommitteeDutyTrace {
		duty, err := c.GetCommitteeDuty(slot, committeeID, spectypes.RoleCommittee)
		require.NoError(t, err)
		_, err = duty.MarshalSSZ()
		require.NoError(t, err, "the trace must encode")
		return duty
	}

	atLimit := make([]byte, traces.MaxProposalDataSize)
	atLimit[0] = 1
	over := make([]byte, traces.MaxProposalDataSize+1)

	t.Run("at the limit", func(t *testing.T) {
		c := newCollector(t)
		require.NoError(t, c.Collect(t.Context(), proposal(atLimit), dummyVerify))
		require.Equal(t, atLimit, archived(t, c).ProposalData)
	})

	t.Run("one byte over", func(t *testing.T) {
		c := newCollector(t)
		require.NoError(t, c.Collect(t.Context(), proposal(over), dummyVerify))
		duty := archived(t, c)
		require.Empty(t, duty.ProposalData)
		require.Len(t, duty.Rounds, 1, "the rest of the trace is kept")
	})

	t.Run("one byte over after a proposal that fits", func(t *testing.T) {
		c := newCollector(t)
		require.NoError(t, c.Collect(t.Context(), proposal(atLimit), dummyVerify))
		require.NoError(t, c.Collect(t.Context(), proposal(over), dummyVerify))
		require.Equal(t, atLimit, archived(t, c).ProposalData)
	})
}
