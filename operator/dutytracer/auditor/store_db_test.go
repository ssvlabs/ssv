package auditor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func ptrRole(r Role) *Role { return &r }

func ptrString(s string) *string { return &s }

func TestDBStore_PutFinding_CapsPerSlotReason(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{Ctx: context.Background()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewDBStore(db)
	slot := uint64(123)
	reason := ReasonScheduleMissingIndex

	for i := 0; i < 10; i++ {
		putRes, err := s.PutFinding(&Finding{
			CreatedAt: time.Now(),
			Slot:      slot,
			Epoch:     3,
			Reason:    reason,
			Evidence:  Evidence{},
		})
		require.NoError(t, err)
		require.True(t, putRes.Stored)
		require.NotEmpty(t, putRes.Key)
	}

	// 11th should be dropped (cap reached) with no error.
	putRes, err := s.PutFinding(&Finding{
		CreatedAt: time.Now(),
		Slot:      slot,
		Epoch:     3,
		Reason:    reason,
		Evidence:  Evidence{},
	})
	require.NoError(t, err)
	require.False(t, putRes.Stored)

	res, err := s.Query(Query{From: phase0.Slot(slot), To: phase0.Slot(slot), Limit: 1000})
	require.NoError(t, err)
	require.Len(t, res.Findings, 10)
}

func TestDBStore_Prune_RemovesOldSlots(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{Ctx: context.Background()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewDBStore(db)

	put := func(slot uint64) {
		putRes, err := s.PutFinding(&Finding{
			CreatedAt: time.Now(),
			Slot:      slot,
			Epoch:     1,
			Reason:    ReasonUnexpectedWireTrace,
			Evidence:  Evidence{},
		})
		require.NoError(t, err)
		require.True(t, putRes.Stored)
	}

	put(10)
	put(11)
	put(12)

	require.NoError(t, s.Prune(phase0.Slot(12)))

	// slots < 12 removed, slot 12 remains.
	res, err := s.Query(Query{From: 10, To: 12, Limit: 1000})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	require.Equal(t, uint64(12), res.Findings[0].Slot)
}

func TestDBStore_Query_ByValidatorIndex_UsesIndex(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{Ctx: context.Background()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewDBStore(db)
	vi := uint64(42)
	viPtr := &vi
	cid := strings.Repeat("01", 32)

	put := func(slot uint64) {
		_, err := s.PutFinding(&Finding{
			CreatedAt:      time.Now(),
			Slot:           slot,
			Epoch:          1,
			Reason:         ReasonUnexpectedWireTrace,
			Role:           ptrRole(RoleAttester),
			ValidatorIndex: viPtr,
			CommitteeID:    ptrString(cid),
			Evidence:       Evidence{},
		})
		require.NoError(t, err)
	}

	put(10)
	put(11)

	res, err := s.Query(Query{From: 0, To: 20, ValidatorIndex: viPtr, Limit: 100})
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
}

func TestDBStore_Prune_RemovesIndexEntries(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{Ctx: context.Background()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := NewDBStore(db)
	vi := uint64(777)
	viPtr := &vi
	cid := strings.Repeat("02", 32)

	put := func(slot uint64) {
		_, err := s.PutFinding(&Finding{
			CreatedAt:      time.Now(),
			Slot:           slot,
			Epoch:          1,
			Reason:         ReasonUnexpectedWireTrace,
			Role:           ptrRole(RoleAttester),
			ValidatorIndex: viPtr,
			CommitteeID:    ptrString(cid),
			Evidence:       Evidence{},
		})
		require.NoError(t, err)
	}

	put(10)
	put(11)

	require.NoError(t, s.Prune(phase0.Slot(11)))

	res, err := s.Query(Query{From: 0, To: 20, ValidatorIndex: viPtr, Limit: 100})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	require.Equal(t, uint64(11), res.Findings[0].Slot)
}
