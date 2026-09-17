package store

import (
	"encoding/binary"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/traces"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func storedFormat(t *testing.T, db basedb.Database) (uint32, bool) {
	t.Helper()
	obj, found, err := db.Get([]byte(traceFormatKey), nil)
	require.NoError(t, err)
	if !found {
		return 0, false
	}
	return binary.LittleEndian.Uint32(obj.Value), true
}

// A fresh or older store is stamped with this binary's format; a store stamped by a newer binary is
// refused, so a downgrade fails at startup instead of losing records to decode errors.
func TestEnsureFormat(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	_, found := storedFormat(t, db)
	require.False(t, found, "an original store carries no format record")
	require.NoError(t, EnsureFormat(db))
	version, found := storedFormat(t, db)
	require.True(t, found)
	require.Equal(t, FormatVersion, version)
	require.NoError(t, EnsureFormat(db), "idempotent at the current format")

	newer := make([]byte, 4)
	binary.LittleEndian.PutUint32(newer, FormatVersion+1)
	require.NoError(t, db.Set([]byte(traceFormatKey), nil, newer))
	require.ErrorContains(t, EnsureFormat(db), "written by a newer exporter")
	version, _ = storedFormat(t, db)
	require.Equal(t, FormatVersion+1, version, "a refused store is left untouched")
}

// A duty that cannot be encoded is left out of the slot's batch and reported; the other duties are saved.
func TestSaveValidatorDuties_SkipsUnencodableDuty(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	defer db.Close()
	s := New(db)

	const slot = phase0.Slot(9)
	good := &traces.ValidatorDutyTrace{Slot: slot, Role: spectypes.BNRoleProposerPreferences, Validator: 1}
	oversized := &traces.ValidatorDutyTrace{Slot: slot, Role: spectypes.BNRoleProposerPreferences, Validator: 2}
	for i := 0; i < traces.MaxPartialSigEntries+1; i++ {
		oversized.Pre = append(oversized.Pre, &traces.PartialSigTrace{Signer: 1})
	}

	saved, err := s.SaveValidatorDuties([]*traces.ValidatorDutyTrace{good, oversized})
	require.ErrorContains(t, err, "index=2")
	require.Equal(t, 1, saved, "only the encodable duty counts as saved")

	got, err := s.GetValidatorDuty(slot, spectypes.BNRoleProposerPreferences, 1)
	require.NoError(t, err)
	require.Equal(t, good.Validator, got.Validator)
	_, err = s.GetValidatorDuty(slot, spectypes.BNRoleProposerPreferences, 2)
	require.ErrorIs(t, err, ErrNotFound)
}
