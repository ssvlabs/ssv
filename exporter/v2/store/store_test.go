package store_test

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	store "github.com/ssvlabs/ssv/exporter/v2/store"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSaveCommitteeDutyTrace(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	trace1 := makeCTrace(1, 'a')
	trace2 := makeCTrace(2, 'b')

	store := store.New(db)
	require.NoError(t, store.SaveCommiteeDuty(trace1))
	require.NoError(t, store.SaveCommiteeDuty(trace2))

	duty, err := store.GetCommitteeDuty(phase0.Slot(1), [32]byte{'a'})
	require.NoError(t, err)
	assert.Equal(t, phase0.Slot(1), duty.Slot)
}

func TestSaveCommitteeDuties(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	traces := []*model.CommitteeDutyTrace{makeCTrace(1, 'a'), makeCTrace(1, 'b')}

	store := store.New(db)
	require.NoError(t, store.SaveCommitteeDuties(phase0.Slot(1), traces))

	duty, err := store.GetCommitteeDuty(phase0.Slot(1), [32]byte{'a'})
	require.NoError(t, err)
	assert.Equal(t, phase0.Slot(1), duty.Slot)

	duty, err = store.GetCommitteeDuty(phase0.Slot(1), [32]byte{'b'})
	require.NoError(t, err)
	assert.Equal(t, phase0.Slot(1), duty.Slot)
}

func TestSaveValidatorDutyTrace(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	trace1 := makeVTrace(1)
	trace2 := makeVTrace(2)

	store := store.New(db)
	require.NoError(t, store.SaveValidatorDuty(trace1))
	require.NoError(t, store.SaveValidatorDuty(trace2))

	trace, err := store.GetValidatorDuty(phase0.Slot(1), types.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(1), trace.Slot)
	require.Equal(t, phase0.ValidatorIndex(39393), trace.Validator)

	trace, err = store.GetValidatorDuty(phase0.Slot(2), types.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(2), trace.Slot)
	require.Equal(t, phase0.ValidatorIndex(39393), trace.Validator)

	_, err = store.GetValidatorDuty(phase0.Slot(3), types.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.Error(t, err)

	traces, err := store.GetAllValidatorDuties(types.BNRoleAttester, phase0.Slot(1))
	require.NoError(t, err)
	require.Len(t, traces, 1)

	traces, err = store.GetAllValidatorDuties(types.BNRoleAttester, phase0.Slot(2))
	require.NoError(t, err)
	require.Len(t, traces, 1)
}

func TestSaveValidatorDuties(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	trace1 := makeVTrace(1)
	trace2 := makeVTrace(2)

	store := store.New(db)
	require.NoError(t, store.SaveValidatorDuties([]*model.ValidatorDutyTrace{trace1, trace2}))

	trace, err := store.GetValidatorDuty(phase0.Slot(1), types.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(1), trace.Slot)
	require.Equal(t, phase0.ValidatorIndex(39393), trace.Validator)

	trace, err = store.GetValidatorDuty(phase0.Slot(2), types.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(2), trace.Slot)
	require.Equal(t, phase0.ValidatorIndex(39393), trace.Validator)

	_, err = store.GetValidatorDuty(phase0.Slot(3), types.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.Error(t, err)

	traces, err := store.GetAllValidatorDuties(types.BNRoleAttester, phase0.Slot(1))
	require.NoError(t, err)
	require.Len(t, traces, 1)

	traces, err = store.GetAllValidatorDuties(types.BNRoleAttester, phase0.Slot(2))
	require.NoError(t, err)
	require.Len(t, traces, 1)
}

func makeVTrace(slot phase0.Slot) *model.ValidatorDutyTrace {
	return &model.ValidatorDutyTrace{
		Slot:      slot,
		Role:      types.BNRoleAttester,
		Validator: phase0.ValidatorIndex(39393),
	}
}

func makeCTrace(slot phase0.Slot, committee byte) *model.CommitteeDutyTrace {
	return &model.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: [32]byte{committee},
		OperatorIDs: nil,
	}
}
