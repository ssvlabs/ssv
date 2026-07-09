package dutytracer

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

// buildAggregatorCommitteeConsensusData builds a minimal, structurally valid
// AggregatorCommitteeConsensusData with one aggregator duty and one sync-committee
// contribution duty, and returns its SSZ encoding.
func buildAggregatorCommitteeConsensusData(t *testing.T) []byte {
	t.Helper()

	att := &phase0.Attestation{
		AggregationBits: []byte{0x01},
		Data: &phase0.AttestationData{
			Slot:            1,
			Index:           0,
			BeaconBlockRoot: phase0.Root{1},
			Source:          &phase0.Checkpoint{Root: phase0.Root{1}},
			Target:          &phase0.Checkpoint{Root: phase0.Root{1}},
		},
		Signature: phase0.BLSSignature{},
	}
	attBytes, err := att.MarshalSSZ()
	require.NoError(t, err)

	data := &spectypes.AggregatorCommitteeConsensusData{
		Version:                     spec.DataVersionPhase0,
		Aggregators:                 []spectypes.AssignedAggregator{{ValidatorIndex: 10, CommitteeIndex: 0, SelectionProof: phase0.BLSSignature{}}},
		AggregatorsCommitteeIndexes: []uint64{0},
		AggregatedAttestations:      [][]byte{attBytes},
		Contributors:                []spectypes.AssignedAggregator{{ValidatorIndex: 20, CommitteeIndex: 0, SelectionProof: phase0.BLSSignature{}}},
		SyncCommitteeContributions: []altair.SyncCommitteeContribution{
			{Slot: 1, BeaconBlockRoot: phase0.Root{1}, SubcommitteeIndex: 0, AggregationBits: make([]byte, 16)},
		},
	}

	enc, err := data.Encode()
	require.NoError(t, err)
	return enc
}

func TestCollector_computeAggregatorCommitteePostConsensusRoles(t *testing.T) {
	collector := New(zap.NewNop(), nil, mockclient{}, nil, networkconfig.TestNetwork.Beacon, nil, nil)

	t.Run("success: aggregator and contribution roots classified distinctly", func(t *testing.T) {
		encoded := buildAggregatorCommitteeConsensusData(t)

		roleByRoot, err := collector.computeAggregatorCommitteePostConsensusRoles(t.Context(), phase0.Slot(1), encoded)
		require.NoError(t, err)
		require.Len(t, roleByRoot, 2)

		var aggCount, sccCount int
		for _, role := range roleByRoot {
			switch role {
			case spectypes.BNRoleAggregator:
				aggCount++
			case spectypes.BNRoleSyncCommitteeContribution:
				sccCount++
			default:
				t.Fatalf("unexpected role %v", role)
			}
		}
		assert.Equal(t, 1, aggCount)
		assert.Equal(t, 1, sccCount)
	})

	t.Run("failure: malformed consensus data returns decode error", func(t *testing.T) {
		_, err := collector.computeAggregatorCommitteePostConsensusRoles(t.Context(), phase0.Slot(1), []byte{1, 2, 3})
		require.Error(t, err)
	})
}

func TestCollector_getAggregatorSelectionRoot(t *testing.T) {
	collector := New(zap.NewNop(), nil, mockclient{}, nil, networkconfig.TestNetwork.Beacon, nil, nil)

	root1, err := collector.getAggregatorSelectionRoot(t.Context(), phase0.Slot(5))
	require.NoError(t, err)
	assert.NotEqual(t, phase0.Root{}, root1)

	// Cached lookup for the same slot must be deterministic.
	root2, err := collector.getAggregatorSelectionRoot(t.Context(), phase0.Slot(5))
	require.NoError(t, err)
	assert.Equal(t, root1, root2)

	// A different slot must yield a different root (distinct SSZUint64 input).
	root3, err := collector.getAggregatorSelectionRoot(t.Context(), phase0.Slot(6))
	require.NoError(t, err)
	assert.NotEqual(t, root1, root3)
}

type errDomainClient struct{}

func (errDomainClient) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return phase0.Domain{}, assert.AnError
}

func TestCollector_getAggregatorSelectionRoot_DomainDataError(t *testing.T) {
	collector := New(zap.NewNop(), nil, errDomainClient{}, nil, networkconfig.TestNetwork.Beacon, nil, nil)

	_, err := collector.getAggregatorSelectionRoot(t.Context(), phase0.Slot(5))
	require.Error(t, err)
}

func TestCommitteeDutyTrace_classifyRootForPending(t *testing.T) {
	rootA := phase0.Root{1}
	rootB := phase0.Root{2}
	rootUnknown := phase0.Root{9, 9, 9}

	t.Run("roleRootsReady takes precedence for sync/attestation roots", func(t *testing.T) {
		dt := &committeeDutyTrace{
			roleRootsReady:    true,
			syncCommitteeRoot: rootA,
			attestationRoot:   rootB,
		}
		role, ok := dt.classifyRootForPending(rootA)
		require.True(t, ok)
		assert.Equal(t, spectypes.BNRoleSyncCommittee, role)

		role, ok = dt.classifyRootForPending(rootB)
		require.True(t, ok)
		assert.Equal(t, spectypes.BNRoleAttester, role)
	})

	t.Run("falls back to aggPostConsensusRoles when roleRootsReady is false", func(t *testing.T) {
		dt := &committeeDutyTrace{
			aggPostConsensusRoles: map[phase0.Root]spectypes.BeaconRole{
				rootA: spectypes.BNRoleAggregator,
				rootB: spectypes.BNRoleSyncCommitteeContribution,
			},
		}
		role, ok := dt.classifyRootForPending(rootA)
		require.True(t, ok)
		assert.Equal(t, spectypes.BNRoleAggregator, role)

		role, ok = dt.classifyRootForPending(rootB)
		require.True(t, ok)
		assert.Equal(t, spectypes.BNRoleSyncCommitteeContribution, role)
	})

	t.Run("unknown root is not classified", func(t *testing.T) {
		dt := &committeeDutyTrace{
			aggPostConsensusRoles: map[phase0.Root]spectypes.BeaconRole{
				rootA: spectypes.BNRoleAggregator,
			},
		}
		_, ok := dt.classifyRootForPending(rootUnknown)
		require.False(t, ok)
	})

	t.Run("empty/nil trace never classifies", func(t *testing.T) {
		dt := &committeeDutyTrace{}
		_, ok := dt.classifyRootForPending(rootA)
		require.False(t, ok)
	})
}

// TestCollector_AggregatorCommitteeDuty_PostConsensusQuorum exercises the full
// AggregatorCommittee post-consensus classification+quorum pipeline through Collect():
// signatures arriving both before and after the proposal must be classified into the
// correct beacon-role bucket (aggregator vs sync-committee-contribution) using distinct
// signing roots, and quorum must only publish once per validator/role once the threshold
// of unique signers for that exact root is met.
func TestCollector_AggregatorCommitteeDuty_PostConsensusQuorum(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot        = phase0.Slot(1)
		vIndexAgg   = phase0.ValidatorIndex(10)
		vIndexContr = phase0.ValidatorIndex(20)
		operator1   = spectypes.OperatorID(1)
		operator2   = spectypes.OperatorID(2)
		operator3   = spectypes.OperatorID(3)
		operator4   = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("agg_committee_pk"), spectypes.RoleAggregatorCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4}, // 4 ops -> quorum at 3
	}

	shareAgg := &ssvtypes.SSVShare{}
	shareAgg.ValidatorPubKey = spectypes.ValidatorPK{1}
	shareContr := &ssvtypes.SSVShare{}
	shareContr.ValidatorPubKey = spectypes.ValidatorPK{2}

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndexAgg).Return(shareAgg, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndexContr).Return(shareContr, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	tracer := New(logger, validators, mockclient{}, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	encodedConsensusData := buildAggregatorCommitteeConsensusData(t)
	roleByRoot, err := tracer.computeAggregatorCommitteePostConsensusRoles(t.Context(), slot, encodedConsensusData)
	require.NoError(t, err)
	require.Len(t, roleByRoot, 2)

	var aggregatorRoot, contributionRoot phase0.Root
	for root, role := range roleByRoot {
		switch role {
		case spectypes.BNRoleAggregator:
			aggregatorRoot = root
		case spectypes.BNRoleSyncCommitteeContribution:
			contributionRoot = root
		default:
			t.Fatalf("unexpected beacon role %v", role)
		}
	}
	require.NotEqual(t, phase0.Root{}, aggregatorRoot)
	require.NotEqual(t, phase0.Root{}, contributionRoot)
	require.NotEqual(t, aggregatorRoot, contributionRoot)

	fakeSig := [96]byte{}

	sendPostConsensus := func(vIdx phase0.ValidatorIndex, op spectypes.OperatorID, root phase0.Root) {
		msgs := &spectypes.PartialSignatureMessages{
			Type: spectypes.PostConsensusPartialSig,
			Slot: slot,
			Messages: []*spectypes.PartialSignatureMessage{
				{ValidatorIndex: vIdx, Signer: op, PartialSignature: fakeSig[:], SigningRoot: root},
			},
		}
		data, encErr := msgs.Encode()
		require.NoError(t, encErr)
		require.NoError(t, tracer.Collect(t.Context(), buildPartialSigMessage(identifier, data), dummyVerify))
	}

	// Step 1: two signers for each duty arrive BEFORE the proposal (below quorum, buffered as pending).
	sendPostConsensus(vIndexAgg, operator1, aggregatorRoot)
	sendPostConsensus(vIndexAgg, operator2, aggregatorRoot)
	sendPostConsensus(vIndexContr, operator1, contributionRoot)
	sendPostConsensus(vIndexContr, operator2, contributionRoot)

	require.Empty(t, listener.GetCalls(), "no quorum should be published before proposal classifies pending roots")

	// Step 2: proposal arrives, deriving post-consensus roles and flushing pending signatures.
	proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
	proposalMsg.SignedSSVMessage.FullData = encodedConsensusData
	require.NoError(t, tracer.Collect(t.Context(), proposalMsg, dummyVerify))

	duty, err := tracer.GetCommitteeDuty(slot, committeeID, spectypes.RoleAggregatorCommittee)
	require.NoError(t, err)
	require.NotNil(t, duty)
	assert.Equal(t, spectypes.RoleAggregatorCommittee, duty.Role)
	// Aggregator role is bucketed under Attester; SyncCommitteeContribution under SyncCommittee.
	require.NotEmpty(t, duty.Attester)
	require.NotEmpty(t, duty.SyncCommittee)

	// Still below quorum (2/4, need 3).
	require.Empty(t, listener.GetCalls(), "2 signers is below quorum threshold")

	// Step 3: third signer arrives for the aggregator duty only -> reaches quorum for vIndexAgg/Aggregator.
	sendPostConsensus(vIndexAgg, operator3, aggregatorRoot)

	calls := listener.GetCalls()
	require.Len(t, calls, 1, "only the aggregator duty should have reached quorum")
	assert.Equal(t, vIndexAgg, calls[0].Index)
	assert.Equal(t, spectypes.BNRoleAggregator, calls[0].Role)
	assert.ElementsMatch(t, []spectypes.OperatorID{operator1, operator2, operator3}, calls[0].Signers)

	// The sync-committee-contribution duty must remain unpublished (still 2/4).
	for _, c := range calls {
		assert.NotEqual(t, spectypes.BNRoleSyncCommitteeContribution, c.Role)
	}

	// Step 4: third signer arrives for the contribution duty -> reaches quorum for vIndexContr/SCC.
	listener.Reset()
	sendPostConsensus(vIndexContr, operator3, contributionRoot)

	calls = listener.GetCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, vIndexContr, calls[0].Index)
	assert.Equal(t, spectypes.BNRoleSyncCommitteeContribution, calls[0].Role)
	assert.ElementsMatch(t, []spectypes.OperatorID{operator1, operator2, operator3}, calls[0].Signers)
}

// TestCollector_AggregatorCommitteeDuty_UnknownRootBuffersUntilProposal ensures that
// post-consensus signatures with a signing root that doesn't match either derived
// AggregatorCommittee role root are buffered (not misclassified) and remain buffered
// even after the proposal arrives, since they don't match any known root.
func TestCollector_AggregatorCommitteeDuty_UnknownRootBuffersUntilProposal(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const slot = phase0.Slot(2)
	identifier := spectypes.NewMsgID([4]byte{}, []byte("agg_committee_pk_2"), spectypes.RoleAggregatorCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{ID: committeeID, Operators: []spectypes.OperatorID{1, 2, 3, 4}}
	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()

	dutyStore := new(mockDutyTraceStore)
	tracer := New(zap.NewNop(), validators, mockclient{}, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)

	encodedConsensusData := buildAggregatorCommitteeConsensusData(t)
	unknownRoot := phase0.Root{123, 123, 123}
	fakeSig := [96]byte{}

	msgs := &spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{
			{ValidatorIndex: 99, Signer: 1, PartialSignature: fakeSig[:], SigningRoot: unknownRoot},
		},
	}
	data, err := msgs.Encode()
	require.NoError(t, err)
	require.NoError(t, tracer.Collect(t.Context(), buildPartialSigMessage(identifier, data), dummyVerify))

	proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
	proposalMsg.SignedSSVMessage.FullData = encodedConsensusData
	require.NoError(t, tracer.Collect(t.Context(), proposalMsg, dummyVerify))

	duty, err := tracer.GetCommitteeDuty(slot, committeeID, spectypes.RoleAggregatorCommittee)
	require.NoError(t, err)
	require.Empty(t, duty.Attester, "unknown root must not be classified as an aggregator signer")
	require.Empty(t, duty.SyncCommittee, "unknown root must not be classified as a sync-committee-contribution signer")
}
