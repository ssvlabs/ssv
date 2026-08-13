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

func TestBlockLogDigests(t *testing.T) {
	s := newTestStorage(t)

	// Absent digest reports found=false (the verifier reads this as "no logs seen there").
	_, found, err := s.GetBlockLogDigest(nil, 42)
	require.NoError(t, err)
	require.False(t, found)

	digest := []byte("some-32-byte-digest-placeholder!")
	require.NoError(t, s.SaveBlockLogDigest(nil, 42, digest))

	got, found, err := s.GetBlockLogDigest(nil, 42)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, digest, got)

	require.NoError(t, s.DeleteBlockLogDigest(nil, 42))
	_, found, err = s.GetBlockLogDigest(nil, 42)
	require.NoError(t, err)
	require.False(t, found)
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
	require.NoError(t, s.SaveBlockLogDigest(nil, 5, []byte("digest")))
	require.NoError(t, s.SetResyncRequired(nil))

	require.NoError(t, s.DropVerificationJournal())

	// Ranges and digests are gone...
	ranges, err := s.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)

	_, found, err := s.GetBlockLogDigest(nil, 5)
	require.NoError(t, err)
	require.False(t, found)

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
