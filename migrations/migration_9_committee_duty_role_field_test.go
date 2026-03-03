package migrations

import (
	"encoding/binary"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	estore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestMigration9CommitteeDutyRoleField(t *testing.T) {
	ctx := t.Context()
	prefix := []byte("cd")

	t.Run("migrates legacy key to role-aware key", func(t *testing.T) {
		opt, err := setupOptions(ctx, t)
		require.NoError(t, err)

		slot := phase0.Slot(123)
		role := spectypes.RoleCommittee
		committeeID := testCommitteeID(0x11)
		legacy := testLegacyCommitteeDuty(slot, committeeID)

		oldKey := testCommitteeLegacyKey(slot, committeeID)
		setCommitteeObj(t, opt.Db, prefix, oldKey, marshalLegacyDuty(t, legacy))

		completedCalled := false
		err = migration_9_migrate_committee_duty_role_field.Run(
			ctx,
			log.TestLogger(t),
			opt,
			[]byte(migration_9_migrate_committee_duty_role_field.Name),
			func(rw basedb.ReadWriter) error {
				completedCalled = true
				return nil
			},
		)
		require.NoError(t, err)
		require.True(t, completedCalled)

		_, oldFound := getCommitteeObj(t, opt.Db, prefix, oldKey)
		require.False(t, oldFound)

		newKey := testCommitteeRoleAwareKey(t, slot, role, committeeID)
		newValue, found := getCommitteeObj(t, opt.Db, prefix, newKey)
		require.True(t, found)

		var migrated exporter.CommitteeDutyTrace
		require.NoError(t, migrated.UnmarshalSSZ(newValue))
		require.Equal(t, slot, migrated.Slot)
		require.Equal(t, role, migrated.Role)
		require.Equal(t, committeeID, migrated.CommitteeID)
		require.Equal(t, []spectypes.OperatorID{1, 2, 3}, migrated.OperatorIDs)
		require.Len(t, migrated.Attester, 1)
		require.Equal(t, spectypes.OperatorID(9), migrated.Attester[0].Signer)
		require.Equal(t, []phase0.ValidatorIndex{100, 101}, migrated.Attester[0].ValidatorIdx)
	})

	t.Run("skips already role-aware keys", func(t *testing.T) {
		opt, err := setupOptions(ctx, t)
		require.NoError(t, err)

		slot := phase0.Slot(321)
		role := spectypes.RoleAggregatorCommittee
		committeeID := testCommitteeID(0x22)
		trace := &exporter.CommitteeDutyTrace{
			Slot:        slot,
			Role:        role,
			CommitteeID: committeeID,
			OperatorIDs: []spectypes.OperatorID{7, 8},
		}
		initialValue, err := trace.MarshalSSZ()
		require.NoError(t, err)

		key := testCommitteeRoleAwareKey(t, slot, role, committeeID)
		setCommitteeObj(t, opt.Db, prefix, key, initialValue)

		err = migration_9_migrate_committee_duty_role_field.Run(
			ctx,
			log.TestLogger(t),
			opt,
			[]byte(migration_9_migrate_committee_duty_role_field.Name),
			func(rw basedb.ReadWriter) error { return nil },
		)
		require.NoError(t, err)

		value, found := getCommitteeObj(t, opt.Db, prefix, key)
		require.True(t, found)
		require.Equal(t, initialValue, value)

		var migrated exporter.CommitteeDutyTrace
		require.NoError(t, migrated.UnmarshalSSZ(value))
		require.Equal(t, slot, migrated.Slot)
		require.Equal(t, role, migrated.Role)
		require.Equal(t, committeeID, migrated.CommitteeID)
	})
}

func testLegacyCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) *migration_9_CommitteeDutyTraceV1 {
	root := [32]byte{}
	root[0] = 0xAA
	return &migration_9_CommitteeDutyTraceV1{
		migration_9_ConsensusTrace: migration_9_ConsensusTrace{
			Decideds: []*migration_9_DecidedTrace{
				{
					Round:        7,
					BeaconRoot:   root,
					Signers:      []uint64{1, 2},
					ReceivedTime: 12345,
				},
			},
		},
		Slot:        uint64(slot),
		CommitteeID: [32]byte(committeeID),
		OperatorIDs: []uint64{1, 2, 3},
		Attester: []*migration_9_SignerData{
			{
				Signer:       9,
				ValidatorIdx: []uint64{100, 101},
				ReceivedTime: 999,
			},
		},
	}
}

func marshalLegacyDuty(t *testing.T, duty *migration_9_CommitteeDutyTraceV1) []byte {
	t.Helper()
	value, err := duty.MarshalSSZ()
	require.NoError(t, err)
	return value
}

func setCommitteeObj(t *testing.T, db basedb.Database, prefix, key, value []byte) {
	t.Helper()
	require.NoError(t, db.Set(prefix, key, value))
}

func getCommitteeObj(t *testing.T, db basedb.Database, prefix, key []byte) ([]byte, bool) {
	t.Helper()
	obj, found, err := db.Get(prefix, key)
	require.NoError(t, err)
	return obj.Value, found
}

func testCommitteeLegacyKey(slot phase0.Slot, committeeID spectypes.CommitteeID) []byte {
	key := make([]byte, 0, 4+len(committeeID))
	key = append(key, slotToBytes(slot)...)
	key = append(key, committeeID[:]...)
	return key
}

func testCommitteeRoleAwareKey(t *testing.T, slot phase0.Slot, role spectypes.RunnerRole, committeeID spectypes.CommitteeID) []byte {
	t.Helper()
	roleByte, err := estore.CommitteeRunnerRoleToPrefix(role)
	require.NoError(t, err)
	key := make([]byte, 0, 4+1+len(committeeID))
	key = append(key, slotToBytes(slot)...)
	key = append(key, roleByte)
	key = append(key, committeeID[:]...)
	return key
}

func slotToBytes(slot phase0.Slot) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(slot))
	return b
}

func testCommitteeID(firstByte byte) spectypes.CommitteeID {
	var committeeID spectypes.CommitteeID
	committeeID[0] = firstByte
	return committeeID
}
