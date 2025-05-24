package storage

import (
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/logging"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func TestEncodeDecodeOperators(t *testing.T) {
	testCases := []struct {
		input   []uint64
		encoded []byte
	}{
		// Valid sizes: 4
		{[]uint64{0x0123456789ABCDEF, 0xFEDCBA9876543210, 0x1122334455667788, 0x8877665544332211},
			[]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0xFE, 0xDC, 0xBA, 0x98, 0x76, 0x54, 0x32, 0x10, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		// Valid sizes: 7
		{[]uint64{1, 2, 3, 4, 5, 6, 7},
			[]byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 7}},
		// Valid sizes: 13
		{[]uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
			[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 9, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 11, 0, 0, 0, 0, 0, 0, 0, 12}},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Case %d", i+1), func(t *testing.T) {
			encoded, err := encodeOperators(tc.input)
			require.Equal(t, err, nil)
			require.Equal(t, tc.encoded, encoded)

			decoded := decodeOperators(encoded)
			require.Equal(t, tc.input, decoded)
		})
	}
}

func Test_mergeParticipantsBitMask(t *testing.T) {
	tests := []struct {
		name          string
		participants1 []spectypes.OperatorID
		participants2 []spectypes.OperatorID
		expected      []spectypes.OperatorID
	}{
		{
			name:          "Both participants empty",
			participants1: []spectypes.OperatorID{},
			participants2: []spectypes.OperatorID{},
			expected:      []spectypes.OperatorID{},
		},
		{
			name:          "First participants empty",
			participants1: []spectypes.OperatorID{},
			participants2: []spectypes.OperatorID{1, 2, 3},
			expected:      []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:          "Second participants empty",
			participants1: []spectypes.OperatorID{1, 2, 3},
			participants2: []spectypes.OperatorID{},
			expected:      []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:          "No duplicates",
			participants1: []spectypes.OperatorID{1, 3, 5},
			participants2: []spectypes.OperatorID{2, 4, 6},
			expected:      []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:          "With duplicates",
			participants1: []spectypes.OperatorID{1, 2, 3, 5},
			participants2: []spectypes.OperatorID{3, 4, 5, 6},
			expected:      []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:          "All duplicates",
			participants1: []spectypes.OperatorID{1, 2, 3},
			participants2: []spectypes.OperatorID{1, 2, 3},
			expected:      []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:          "Large participants size",
			participants1: []spectypes.OperatorID{1, 3, 5, 7, 9, 11, 13},
			participants2: []spectypes.OperatorID{2, 4, 6, 8, 10, 12},
			expected:      []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
		},
	}

	for _, tt := range tests {
		committee := []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}

		t.Run(tt.name, func(t *testing.T) {
			q1 := qbftstorage.Quorum{
				Signers:   tt.participants1,
				Committee: committee,
			}

			q2 := qbftstorage.Quorum{
				Signers:   tt.participants2,
				Committee: committee,
			}

			result := mergeParticipantsBitMask(q1.ToSignersBitMask(), q2.ToSignersBitMask())
			signers, err := result.Signers(committee)
			require.NoError(t, err)
			require.Equal(t, tt.expected, signers)
		})
	}
}

// TestUpsertSlot validates the interval upsertion logic without any DB dependency.
func TestUpsertSlot(t *testing.T) {
	store := &ibftStorage{} // no DB needed for in-memory logic
	m := make(map[phase0.Slot][]spectypes.OperatorID)

	// 1. Empty history: first insert
	changed := store.upsertSlot(m, 5, []spectypes.OperatorID{1, 2, 3})
	require.True(t, changed)
	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, m[5])

	// 2. Insert same as existing predecessor: no change
	changed = store.upsertSlot(m, 7, []spectypes.OperatorID{1, 2, 3})
	require.False(t, changed)

	// 3. Insert different after existing: new boundary
	changed = store.upsertSlot(m, 10, []spectypes.OperatorID{4, 5})
	require.True(t, changed)
	require.Equal(t, []spectypes.OperatorID{4, 5}, m[10])

	// 4. Insert matching next interval: should merge backward
	// (the interval at start=10 has {4,5})
	changed = store.upsertSlot(m, 8, []spectypes.OperatorID{4, 5})
	require.True(t, changed)
	require.Contains(t, m, phase0.Slot(8))
	require.NotContains(t, m, phase0.Slot(10))

	// 5. Insert in between different intervals: fresh boundary
	changed = store.upsertSlot(m, 6, []spectypes.OperatorID{7})
	require.True(t, changed)
	require.Equal(t, []spectypes.OperatorID{7}, m[6])
}

// helper to create a new ibftStorage with an in-memory DB
func newTestStorage(t *testing.T) *ibftStorage {
	db, err := kv.NewInMemory(logging.TestLogger(t), basedb.Options{})
	require.NoError(t, err)

	store := &ibftStorage{
		prefix: []byte("test:"),
		db:     db,
	}
	return store
}

func TestSaveAndGetCommittee(t *testing.T) {
	store := newTestStorage(t)
	id := convert.MessageID([56]byte{1, 2, 3})

	// Save committees at slots 10, 20, 30
	req1 := []spectypes.OperatorID{11, 12}
	req2 := []spectypes.OperatorID{21}
	req3 := []spectypes.OperatorID{31, 32, 33}

	err := store.saveCommittee(nil, id, 10, req1)
	require.NoError(t, err)
	err = store.saveCommittee(nil, id, 20, req2)
	require.NoError(t, err)
	err = store.saveCommittee(nil, id, 30, req3)
	require.NoError(t, err)

	// Exact slot retrieval
	op, err := store.getCommittee(nil, id, 20)
	require.NoError(t, err)
	require.Equal(t, req2, op)

	// Between slots: 25 → should get slot 20
	op, err = store.getCommittee(nil, id, 25)
	require.NoError(t, err)
	require.Equal(t, req2, op)

	// After last: 100 → slot 30
	op, err = store.getCommittee(nil, id, 100)
	require.NoError(t, err)
	require.Equal(t, req3, op)

	// Before first: 5 → error
	op, err = store.getCommittee(nil, id, 5)
	require.Error(t, err)
	require.Nil(t, op)

	// No history for new id
	newID := convert.MessageID([56]byte{4, 5, 6})
	op, err = store.getCommittee(nil, newID, 1)
	require.NoError(t, err)
	require.Nil(t, op)
}
