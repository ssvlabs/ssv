package migrations

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	estore "github.com/ssvlabs/ssv/exporter/store"
	traces "github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/storage/basedb"
	pebbledb "github.com/ssvlabs/ssv/storage/pebble"
)

func TestMigration9CommitteeDutyRoleField(t *testing.T) {
	prefix := []byte("cd")

	backends := []struct {
		name  string
		setup func(t *testing.T) Options
	}{
		{name: "badger", setup: setupOptionsBadger},
		{name: "pebble", setup: setupOptionsPebble},
	}

	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			t.Run("migrates legacy key to role-aware key", func(t *testing.T) {
				testMigration9MigratesLegacyKey(t, backend.setup(t), prefix)
			})

			t.Run("skips already role-aware keys", func(t *testing.T) {
				testMigration9SkipsRoleAwareKeys(t, backend.setup(t), prefix)
			})

			t.Run("batches rewrites across multiple chunks", func(t *testing.T) {
				testMigration9BatchesAcrossChunks(t, backend.setup(t), prefix)
			})
		})
	}
}

func setupOptionsBadger(t *testing.T) Options {
	t.Helper()
	opt, err := setupOptions(t.Context(), t)
	require.NoError(t, err)
	return opt
}

func setupOptionsPebble(t *testing.T) Options {
	t.Helper()
	db, err := pebbledb.New(log.TestLogger(t), t.TempDir(), &pebble.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return Options{
		Db:     db,
		DbPath: t.TempDir(),
	}
}

func testMigration9MigratesLegacyKey(t *testing.T, opt Options, prefix []byte) {
	ctx := t.Context()

	slot := phase0.Slot(123)
	role := spectypes.RoleCommittee
	committeeID := testCommitteeID(0x11)
	legacy := testLegacyCommitteeDuty(slot, committeeID)

	oldKey := testCommitteeLegacyKey(slot, committeeID)
	setCommitteeObj(t, opt.Db, prefix, oldKey, marshalLegacyDuty(t, legacy))

	completedCalled := false
	err := migration_9_migrate_committee_duty_role_field.Run(
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

	var migrated traces.CommitteeDutyTrace
	require.NoError(t, migrated.UnmarshalSSZ(newValue))
	require.Equal(t, slot, migrated.Slot)
	require.Equal(t, role, migrated.Role)
	require.Equal(t, committeeID, migrated.CommitteeID)
	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, migrated.OperatorIDs)
	require.Len(t, migrated.Attester, 1)
	require.Equal(t, spectypes.OperatorID(9), migrated.Attester[0].Signer)
	require.Equal(t, []phase0.ValidatorIndex{100, 101}, migrated.Attester[0].ValidatorIdx)
}

func testMigration9SkipsRoleAwareKeys(t *testing.T, opt Options, prefix []byte) {
	ctx := t.Context()

	slot := phase0.Slot(321)
	role := spectypes.RoleAggregatorCommittee
	committeeID := testCommitteeID(0x22)
	trace := &traces.CommitteeDutyTrace{
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

	var migrated traces.CommitteeDutyTrace
	require.NoError(t, migrated.UnmarshalSSZ(value))
	require.Equal(t, slot, migrated.Slot)
	require.Equal(t, role, migrated.Role)
	require.Equal(t, committeeID, migrated.CommitteeID)
}

// testMigration9BatchesAcrossChunks writes more than two full migrationBatchSize chunks'
// worth of legacy records and verifies the migration correctly flushes multiple batches:
// every old key is removed, every new key exists with the correct round-tripped value, and
// the number of migrated records matches exactly.
func testMigration9BatchesAcrossChunks(t *testing.T, opt Options, prefix []byte) {
	ctx := t.Context()
	const numRecords = migrationBatchSize*2 + 137

	committeeID := testCommitteeID(0x33)

	type record struct {
		slot   phase0.Slot
		oldKey []byte
	}
	records := make([]record, 0, numRecords)
	for i := 0; i < numRecords; i++ {
		slot := phase0.Slot(i)
		legacy := testLegacyCommitteeDuty(slot, committeeID)
		oldKey := testCommitteeLegacyKey(slot, committeeID)
		setCommitteeObj(t, opt.Db, prefix, oldKey, marshalLegacyDuty(t, legacy))
		records = append(records, record{slot: slot, oldKey: oldKey})
	}

	err := migration_9_migrate_committee_duty_role_field.Run(
		ctx,
		log.TestLogger(t),
		opt,
		[]byte(migration_9_migrate_committee_duty_role_field.Name),
		func(rw basedb.ReadWriter) error { return nil },
	)
	require.NoError(t, err)

	migratedCount := 0
	for _, rec := range records {
		_, oldFound := getCommitteeObj(t, opt.Db, prefix, rec.oldKey)
		require.False(t, oldFound)

		newKey := testCommitteeRoleAwareKey(t, rec.slot, spectypes.RoleCommittee, committeeID)
		newValue, found := getCommitteeObj(t, opt.Db, prefix, newKey)
		require.True(t, found)

		var migrated traces.CommitteeDutyTrace
		require.NoError(t, migrated.UnmarshalSSZ(newValue))
		require.Equal(t, rec.slot, migrated.Slot)
		require.Equal(t, spectypes.RoleCommittee, migrated.Role)
		require.Equal(t, committeeID, migrated.CommitteeID)

		migratedCount++
	}
	require.Equal(t, numRecords, migratedCount)
}

// migration9GoldenTraceHex is the pinned serialization of the fixture below, produced by
// traces.CommitteeDutyTrace.MarshalSSZ at the time migration_9 shipped.
const migration9GoldenTraceHex = "48000000cc0100002a00000000000000000000000000000044000000000000000000000000000000000000000000000000000000000000001c0200003c0200004002000060020000040000000100000000000000180000001001000048010000800100000200000000000000aabb0000000000000000000000000000000000000000000000000000000000000300000000000000570400000000000040000000c0000000040000000200000000000000aabb000000000000000000000000000000000000000000000000000000000000030000000000000057040000000000000100000000000000440000000200000000000000aabb000000000000000000000000000000000000000000000000000000000000030000000000000057040000000000000200000000000000aabb000000000000000000000000000000000000000000000000000000000000030000000000000057040000000000000200000000000000aabb000000000000000000000000000000000000000000000000000000000000030000000000000057040000000000000200000000000000aabb00000000000000000000000000000000000000000000000000000000000003000000000000005704000000000000040000000200000000000000aabb00000000000000000000000000000000000000000000000000000000000034000000ae080000000000000100000000000000020000000000000003000000000000000100000000000000020000000000000003000000000000000400000000000000deadbeef04000000050000000000000014000000050d000000000000c800000000000000040000000600000000000000140000005c1100000000000064000000000000006500000000000000"

// TestMigration9TargetLayoutIsFrozen pins the byte layout of migration_9's write side.
//
// The legacy source model is frozen (migration_9_CommitteeDutyTraceV1 with its own generated
// encoding), but the target is the LIVE traces.CommitteeDutyTrace and the LIVE
// estore.CommitteeRunnerRoleToPrefix map. If this test fails, one of them changed shape:
// freeze migration_9's target into its own V2 model rather than letting an old-DB upgrade
// silently emit the newest format, which a later migration would not expect as its input.
func TestMigration9TargetLayoutIsFrozen(t *testing.T) {
	root := phase0.Root{0xAA, 0xBB}
	qbft := traces.QBFTTrace{
		Round:        2,
		BeaconRoot:   root,
		Signer:       3,
		ReceivedTime: 1111,
	}
	trace := &traces.CommitteeDutyTrace{
		ConsensusTrace: traces.ConsensusTrace{
			Rounds: []*traces.RoundTrace{
				{
					Proposer: 1,
					ProposalTrace: &traces.ProposalTrace{
						QBFTTrace: qbft,
						RoundChanges: []*traces.RoundChangeTrace{
							{
								QBFTTrace:       qbft,
								PreparedRound:   1,
								PrepareMessages: []*traces.QBFTTrace{&qbft},
							},
						},
						PrepareMessages: []*traces.QBFTTrace{&qbft},
					},
					Prepares:     []*traces.QBFTTrace{&qbft},
					Commits:      []*traces.QBFTTrace{&qbft},
					RoundChanges: nil,
				},
			},
			Decideds: []*traces.DecidedTrace{
				{
					Round:        2,
					BeaconRoot:   root,
					Signers:      []spectypes.OperatorID{1, 2, 3},
					ReceivedTime: 2222,
				},
			},
		},
		Slot:         42,
		Role:         spectypes.RoleCommittee,
		CommitteeID:  testCommitteeID(0x44),
		OperatorIDs:  []spectypes.OperatorID{1, 2, 3, 4},
		ProposalData: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		SyncCommittee: []*traces.SignerData{
			{Signer: 5, ValidatorIdx: []phase0.ValidatorIndex{200}, ReceivedTime: 3333},
		},
		Attester: []*traces.SignerData{
			{Signer: 6, ValidatorIdx: []phase0.ValidatorIndex{100, 101}, ReceivedTime: 4444},
		},
	}

	value, err := trace.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, migration9GoldenTraceHex, hex.EncodeToString(value),
		"traces.CommitteeDutyTrace serialized layout changed")

	roleByte, err := estore.CommitteeRunnerRoleToPrefix(spectypes.RoleCommittee)
	require.NoError(t, err)
	require.Equal(t, byte(0x00), roleByte,
		"CommitteeRunnerRoleToPrefix(RoleCommittee) changed")
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
