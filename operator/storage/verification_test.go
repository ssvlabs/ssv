package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func newTestStorage(t *testing.T) *storage {
	db, err := kv.NewInMemory(log.TestLogger(t), basedb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &storage{db: db}
}

func TestUnverifiedRanges(t *testing.T) {
	s := newTestStorage(t)

	// Empty to start.
	ranges, err := s.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)

	// Saved ranges come back ordered by From, regardless of insertion order.
	require.NoError(t, s.SaveUnverifiedRange(nil, UnverifiedRange{From: 100, To: 200, Cursor: 100}))
	require.NoError(t, s.SaveUnverifiedRange(nil, UnverifiedRange{From: 10, To: 20, Cursor: 15}))
	ranges, err = s.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Equal(t, []UnverifiedRange{
		{From: 10, To: 20, Cursor: 15},
		{From: 100, To: 200, Cursor: 100},
	}, ranges)

	// Saving a range with an existing From upserts it (used to persist cursor progress).
	require.NoError(t, s.SaveUnverifiedRange(nil, UnverifiedRange{From: 10, To: 20, Cursor: 18}))
	ranges, err = s.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Len(t, ranges, 2)
	require.Equal(t, uint64(18), ranges[0].Cursor)

	// Deleting removes just that range.
	require.NoError(t, s.DeleteUnverifiedRange(nil, 10))
	ranges, err = s.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Equal(t, []UnverifiedRange{{From: 100, To: 200, Cursor: 100}}, ranges)
}

func TestBlockLogIDs(t *testing.T) {
	s := newTestStorage(t)

	// Absent IDs report found=false (the verifier reads this as "no logs seen there").
	_, found, err := s.GetBlockLogIDs(nil, 42)
	require.NoError(t, err)
	require.False(t, found)

	ids := []byte{0, 0, 2, 5}
	require.NoError(t, s.SaveBlockLogIDs(nil, 42, ids))

	got, found, err := s.GetBlockLogIDs(nil, 42)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ids, got)

	require.NoError(t, s.DeleteBlockLogIDs(nil, 42))
	_, found, err = s.GetBlockLogIDs(nil, 42)
	require.NoError(t, err)
	require.False(t, found)
}

func TestUnsettledBlocks(t *testing.T) {
	s := newTestStorage(t)

	blocks, err := s.ListUnsettledBlocks(nil)
	require.NoError(t, err)
	require.Empty(t, blocks)

	// Listed in ascending order regardless of insertion order; saving twice is idempotent.
	for _, block := range []uint64{300, 7, 1 << 40, 7} {
		require.NoError(t, s.SaveUnsettledBlock(nil, block))
	}
	blocks, err = s.ListUnsettledBlocks(nil)
	require.NoError(t, err)
	require.Equal(t, []uint64{7, 300, 1 << 40}, blocks)

	require.NoError(t, s.DeleteUnsettledBlock(nil, 300))
	blocks, err = s.ListUnsettledBlocks(nil)
	require.NoError(t, err)
	require.Equal(t, []uint64{7, 1 << 40}, blocks)
}

func TestResyncRequiredFlag(t *testing.T) {
	s := newTestStorage(t)

	set, err := s.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, set)

	require.NoError(t, s.SetResyncRequired(nil))
	set, err = s.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, set)
}

func TestResyncInProgressFlag(t *testing.T) {
	s := newTestStorage(t)

	set, err := s.IsResyncInProgress(nil)
	require.NoError(t, err)
	require.False(t, set)

	require.NoError(t, s.SetResyncInProgress(nil))
	set, err = s.IsResyncInProgress(nil)
	require.NoError(t, err)
	require.True(t, set)

	require.NoError(t, s.ClearResyncInProgress(nil))
	set, err = s.IsResyncInProgress(nil)
	require.NoError(t, err)
	require.False(t, set)
}

func TestLastResyncTime(t *testing.T) {
	s := newTestStorage(t)

	_, found, err := s.GetLastResyncTime(nil)
	require.NoError(t, err)
	require.False(t, found)

	now := time.Now().Truncate(time.Second)
	require.NoError(t, s.SetLastResyncTime(nil, now))

	got, found, err := s.GetLastResyncTime(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, now.Unix(), got.Unix())
}

func TestDropVerificationJournal(t *testing.T) {
	s := newTestStorage(t)

	require.NoError(t, s.SaveUnverifiedRange(nil, UnverifiedRange{From: 1, To: 9, Cursor: 1}))
	require.NoError(t, s.SaveBlockLogIDs(nil, 5, []byte{0, 0}))
	require.NoError(t, s.SaveUnsettledBlock(nil, 6))
	require.NoError(t, s.SetResyncRequired(nil))

	require.NoError(t, s.DropVerificationJournal())

	// Ranges, log IDs and unsettled blocks are gone...
	ranges, err := s.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)

	_, found, err := s.GetBlockLogIDs(nil, 5)
	require.NoError(t, err)
	require.False(t, found)

	unsettled, err := s.ListUnsettledBlocks(nil)
	require.NoError(t, err)
	require.Empty(t, unsettled)

	// ...but the resync flag is deliberately left set (cleared only after a verified resync).
	set, err := s.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, set)

	// ClearResyncRequired clears it.
	require.NoError(t, s.ClearResyncRequired(nil))
	set, err = s.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, set)
}
