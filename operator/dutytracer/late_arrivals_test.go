package validator

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/exporter/v2/store"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
)

func TestSaveLateTraces(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot         = phase0.Slot(1)
		role, bnRole = spectypes.RoleAggregator, spectypes.BNRoleAggregator
		vIndex       = phase0.ValidatorIndex(55)
	)

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)

	collector := New(context.TODO(), logger, nil, nil, dutyStore, networkconfig.TestNetwork.Beacon.GetBeaconNetwork())

	// Create test traces with specific slots
	diskCommitteeDuty := &model.CommitteeDutyTrace{
		CommitteeID: spectypes.CommitteeID{1},
		Slot:        phase0.Slot(3),
		ConsensusTrace: model.ConsensusTrace{
			Decideds: []*model.DecidedTrace{
				{
					Round:      1,
					BeaconRoot: phase0.Root{1},
					Signers:    []spectypes.OperatorID{111},
				},
			},
		},
	}
	dutyStore.SaveCommitteeDuty(diskCommitteeDuty)

	collector.lateCommitteeTraces = []*committeeDutyTrace{{
		CommitteeDutyTrace: model.CommitteeDutyTrace{
			CommitteeID:  spectypes.CommitteeID{1},
			Slot:         phase0.Slot(3),
			ProposalData: []byte{3, 4, 1},
			ConsensusTrace: model.ConsensusTrace{
				Decideds: []*model.DecidedTrace{
					{
						Round:      1,
						BeaconRoot: phase0.Root{1},
						Signers:    []spectypes.OperatorID{222},
					},
				},
			},
		},
	}, {
		CommitteeDutyTrace: model.CommitteeDutyTrace{
			CommitteeID: spectypes.CommitteeID{2},
			Slot:        phase0.Slot(5),
		},
	},
	}

	// Create test validator traces
	diskValidatorDuty := &model.ValidatorDutyTrace{
		Slot:      phase0.Slot(4),
		Role:      bnRole,
		Validator: vIndex,
		ConsensusTrace: model.ConsensusTrace{
			Decideds: []*model.DecidedTrace{
				{
					Round:      1,
					BeaconRoot: phase0.Root{1},
					Signers:    []spectypes.OperatorID{555},
				},
			},
		},
	}
	dutyStore.SaveValidatorDuty(diskValidatorDuty)

	collector.lateValidatorTraces = []*validatorDutyTrace{{
		Roles: []*model.ValidatorDutyTrace{{
			Slot:         phase0.Slot(4),
			Role:         bnRole,
			Validator:    vIndex,
			ProposalData: []byte{1, 2, 3},
			ConsensusTrace: model.ConsensusTrace{
				Decideds: []*model.DecidedTrace{
					{
						Round:      1,
						BeaconRoot: phase0.Root{1},
						Signers:    []spectypes.OperatorID{666},
					},
				},
			},
		}},
	}, {
		Roles: []*model.ValidatorDutyTrace{
			{
				Slot:      phase0.Slot(5),
				Role:      bnRole,
				Validator: vIndex + 1,
			},
		},
	},
	}

	// links
	collector.lateLinks = []validatorCommitteeLink{
		{
			slot:           phase0.Slot(2),
			validatorIndex: vIndex,
			committeeID:    spectypes.CommitteeID{1, 2, 3},
		},
		{
			slot:           phase0.Slot(9),
			validatorIndex: vIndex,
			committeeID:    spectypes.CommitteeID{7, 8, 9},
		},
		{
			slot:           phase0.Slot(3),
			validatorIndex: vIndex,
			committeeID:    spectypes.CommitteeID{3, 4, 5},
		},
	}

	// Evict late traces
	collector.lateArrivalThreshold = 0
	collector.evictLateTraces(4)

	// Verify traces were removed from memory
	if len(collector.lateCommitteeTraces) != 1 {
		t.Errorf("expected 0 committee traces in memory, got %d", len(collector.lateCommitteeTraces))
	}
	if len(collector.lateValidatorTraces) != 1 {
		t.Errorf("expected 0 validator traces in memory, got %d", len(collector.lateValidatorTraces))
	}

	// evicted committee traces should be removed from cache
	for _, trace := range collector.lateCommitteeTraces {
		duty, err := dutyStore.GetCommitteeDuty(trace.Slot, trace.CommitteeID)
		require.Error(t, err)
		require.Nil(t, duty)
	}

	// Verify committee traces were updated on disk
	committeDuty, err := dutyStore.GetCommitteeDuty(diskCommitteeDuty.Slot, diskCommitteeDuty.CommitteeID)
	require.NoError(t, err)
	require.NotNil(t, committeDuty)

	assert.ElementsMatch(t, committeDuty.Decideds, []*model.DecidedTrace{
		{
			Round:      1,
			BeaconRoot: phase0.Root{1},
			Signers:    []spectypes.OperatorID{111},
		},
		{
			Round:      1,
			BeaconRoot: phase0.Root{1},
			Signers:    []spectypes.OperatorID{222},
		},
	})

	// Verify validator traces were removed from memory
	for _, trace := range collector.lateValidatorTraces {
		for _, role := range trace.Roles {
			validatorDuty, err := dutyStore.GetValidatorDuty(role.Slot, role.Role, role.Validator)
			require.Error(t, err)
			require.Nil(t, validatorDuty)
		}
	}

	// Verify validator traces were updated on disk
	validatorDuty, err := dutyStore.GetValidatorDuty(diskValidatorDuty.Slot, bnRole, vIndex)
	require.NoError(t, err)
	require.NotNil(t, validatorDuty)

	assert.Equal(t, validatorDuty.ProposalData, []byte{1, 2, 3})
	assert.ElementsMatch(t, validatorDuty.Decideds, []*model.DecidedTrace{
		{
			Round:      1,
			BeaconRoot: phase0.Root{1},
			Signers:    []spectypes.OperatorID{555},
		},
		{
			Round:      1,
			BeaconRoot: phase0.Root{1},
			Signers:    []spectypes.OperatorID{666},
		},
	})

	// Verify links were removed from memory
	assert.Len(t, collector.lateLinks, 1)
	assert.Equal(t, collector.lateLinks[0].committeeID, spectypes.CommitteeID{7, 8, 9})

	// Verify links were moved to disk
	link, err := collector.getCommitteeIDFromDisk(phase0.Slot(2), vIndex)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, link, spectypes.CommitteeID{1, 2, 3})

	link, err = collector.getCommitteeIDFromDisk(phase0.Slot(3), vIndex)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, link, spectypes.CommitteeID{3, 4, 5})

	_, err = collector.getCommitteeIDFromDisk(phase0.Slot(9), vIndex)
	require.Error(t, err)
}
