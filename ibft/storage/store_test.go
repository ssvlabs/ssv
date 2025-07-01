package storage

import (
	"context"
	"crypto/rsa"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/operator/slotticker"
	mockslotticker "github.com/ssvlabs/ssv/operator/slotticker/mocks"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestRemoveSlot(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	t.Cleanup(func() { _ = db.Close() })
	assert.NoError(t, err)

	role := spectypes.BNRoleAttester

	ibftStorage := NewStores()
	ibftStorage.Add(role, New(zap.NewNop(), db, role))

	_ = bls.Init(bls.BLS12_381)

	sks, op, rsaKeys := GenerateNodes(4)
	oids := make([]spectypes.OperatorID, 0, len(op))
	for _, o := range op {
		oids = append(oids, o.OperatorID)
	}

	pk := sks[1].GetPublicKey()
	decided250Seq, err := protocoltesting.CreateMultipleStoredInstances(rsaKeys, specqbft.Height(0), specqbft.Height(250), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
		return oids, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: pk.Serialize(),
			Root:       [32]byte{0x1, 0x2, 0x3},
		}
	})
	require.NoError(t, err)

	storage := ibftStorage.Get(role).(*participantStorage)

	// save participants
	for _, d := range decided250Seq {
		_, err := storage.SaveParticipants(
			spectypes.ValidatorPK(pk.Serialize()),
			phase0.Slot(d.State.Height),
			qbftstorage.Quorum{
				Signers:   d.DecidedMessage.OperatorIDs,
				Committee: oids,
			},
		)
		require.NoError(t, err)
	}

	t.Run("should have all participants", func(t *testing.T) {
		pp, err := storage.GetAllParticipantsInRange(phase0.Slot(0), phase0.Slot(250))
		require.Nil(t, err)
		require.Equal(t, 251, len(pp)) // seq 0 - 250
	})

	t.Run("remove slot older than", func(t *testing.T) {
		threshold := phase0.Slot(100)

		count := storage.removeSlotsOlderThan(threshold)
		require.Equal(t, 100, count)

		pp, err := storage.GetAllParticipantsInRange(phase0.Slot(0), phase0.Slot(250))
		require.Nil(t, err)
		require.Equal(t, 151, len(pp)) // seq 0 - 150

		found := slices.ContainsFunc(pp, func(e qbftstorage.ParticipantsRangeEntry) bool {
			return e.Slot < threshold
		})

		assert.False(t, found, "found slots, none expected")
	})

	t.Run("remove slot at", func(t *testing.T) {
		count, err := storage.removeSlotAt(phase0.Slot(150))
		require.Nil(t, err)
		assert.Equal(t, 1, count)
		pp, err := storage.GetAllParticipantsInRange(phase0.Slot(0), phase0.Slot(250))
		require.Nil(t, err)
		require.Equal(t, 150, len(pp)) // seq 0 - 149
	})
}

func TestSlotCleanupJob(t *testing.T) {
	// to test the slot cleanup job we insert 10 unique slots with two pubkey entries
	// per slot, then we configure the job to retain only 1 slot in the past
	// at boot the job removes ALL slots lower than the retained one, ex: if ticker
	// returns slot 4 - slots 0,1 and 2 will be deleted and slot 3 retained
	// and then on next tick (5) slot 3 will be removed as well keeping back slot 4.

	// setup
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	assert.NoError(t, err)

	role := spectypes.BNRoleAttester

	ibftStorage := NewStores()
	ibftStorage.Add(role, New(zap.NewNop(), db, role))

	_ = bls.Init(bls.BLS12_381)

	sks, op, rsaKeys := GenerateNodes(4)
	oids := make([]spectypes.OperatorID, 0, len(op))
	for _, o := range op {
		oids = append(oids, o.OperatorID)
	}

	// pk 1
	pk := sks[1].GetPublicKey()
	decided10Seq, err := protocoltesting.CreateMultipleStoredInstances(rsaKeys, specqbft.Height(0), specqbft.Height(9), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
		return oids, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: pk.Serialize(),
			Root:       [32]byte{0x1, 0x2, 0x3},
		}
	})
	require.NoError(t, err)

	storage := ibftStorage.Get(role).(*participantStorage)

	// save participants
	for _, d := range decided10Seq {
		_, err := storage.SaveParticipants(
			spectypes.ValidatorPK(pk.Serialize()),
			phase0.Slot(d.State.Height),
			qbftstorage.Quorum{
				Signers:   d.DecidedMessage.OperatorIDs,
				Committee: oids,
			},
		)
		require.NoError(t, err)
	}

	// pk 2
	pk = sks[2].GetPublicKey()
	decided10Seq, err = protocoltesting.CreateMultipleStoredInstances(rsaKeys, specqbft.Height(0), specqbft.Height(9), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
		return oids, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: pk.Serialize(),
			Root:       [32]byte{0x1, 0x2, 0x3},
		}
	})
	require.NoError(t, err)

	for _, d := range decided10Seq {
		_, err := storage.SaveParticipants(
			spectypes.ValidatorPK(pk.Serialize()),
			phase0.Slot(d.State.Height),
			qbftstorage.Quorum{
				Signers:   d.DecidedMessage.OperatorIDs,
				Committee: oids,
			},
		)
		require.NoError(t, err)
	}

	// test
	ctx, cancel := context.WithCancel(t.Context())

	ctrl := gomock.NewController(t)
	ticker := mockslotticker.NewMockSlotTicker(ctrl)

	mockTimeChan := make(chan time.Time)
	mockSlotChan := make(chan phase0.Slot)
	t.Cleanup(func() {
		close(mockSlotChan)
		close(mockTimeChan)
	})

	ticker.EXPECT().Next().Return(mockTimeChan).AnyTimes()
	ticker.EXPECT().Slot().DoAndReturn(func() phase0.Slot {
		return <-mockSlotChan
	}).AnyTimes()

	tickerProv := func() slotticker.SlotTicker {
		return ticker
	}

	// initial cleanup removes ALL slots below 3
	storage.Prune(ctx, 3)

	pp, err := storage.GetAllParticipantsInRange(phase0.Slot(0), phase0.Slot(10))
	require.Nil(t, err)
	require.Equal(t, 14, len(pp))

	// 	0	1	2	3	4	5	6	7	8	9
	//	x   x   x	~
	assert.Equal(t, phase0.Slot(3), pp[0].Slot)
	assert.Equal(t, phase0.Slot(3), pp[1].Slot)
	assert.Equal(t, phase0.Slot(9), pp[12].Slot)
	assert.Equal(t, phase0.Slot(9), pp[13].Slot)

	// run normal gc
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		storage.PruneContinously(ctx, tickerProv, 1)
	}()

	mockTimeChan <- time.Now()
	mockSlotChan <- phase0.Slot(5)

	cancel()
	wg.Wait()

	pp, err = storage.GetAllParticipantsInRange(phase0.Slot(0), phase0.Slot(10))
	require.Nil(t, err)
	require.Equal(t, 12, len(pp))

	// 	0	1	2	3	4	5	6	7	8	9
	//	x   x   x	x	~
	assert.Equal(t, phase0.Slot(4), pp[0].Slot)
	assert.Equal(t, phase0.Slot(4), pp[1].Slot)
	assert.Equal(t, phase0.Slot(9), pp[10].Slot)
	assert.Equal(t, phase0.Slot(9), pp[11].Slot)
}

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
	store := &participantStorage{} // no DB needed for in-memory logic
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
func newTestStorage(t *testing.T) *participantStorage {
	db, err := kv.NewInMemory(logging.TestLogger(t), basedb.Options{})
	require.NoError(t, err)

	store := &participantStorage{
		prefix: []byte("test:"),
		db:     db,
	}
	return store
}

func TestSaveAndGetCommittee(t *testing.T) {
	store := newTestStorage(t)
	pk := spectypes.ValidatorPK{1, 2, 3}

	// Save committees at slots 10, 20, 30
	req1 := []spectypes.OperatorID{11, 12}
	req2 := []spectypes.OperatorID{21}
	req3 := []spectypes.OperatorID{31, 32, 33}

	err := store.saveCommittee(nil, pk, 10, req1)
	require.NoError(t, err)
	err = store.saveCommittee(nil, pk, 20, req2)
	require.NoError(t, err)
	err = store.saveCommittee(nil, pk, 30, req3)
	require.NoError(t, err)

	// Exact slot retrieval
	op, err := store.getCommittee(nil, pk, 20)
	require.NoError(t, err)
	require.Equal(t, req2, op)

	// Between slots: 25 → should get slot 20
	op, err = store.getCommittee(nil, pk, 25)
	require.NoError(t, err)
	require.Equal(t, req2, op)

	// After last: 100 → slot 30
	op, err = store.getCommittee(nil, pk, 100)
	require.NoError(t, err)
	require.Equal(t, req3, op)

	// Before first: 5 → error
	op, err = store.getCommittee(nil, pk, 5)
	require.Error(t, err)
	require.Nil(t, op)

	// No history for new pk
	newPK := spectypes.ValidatorPK{4, 5, 6}
	op, err = store.getCommittee(nil, newPK, 1)
	require.NoError(t, err)
	require.Nil(t, op)
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, []*spectypes.Operator, []*rsa.PrivateKey) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make([]*spectypes.Operator, 0, cnt)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	rsaKeys := make([]*rsa.PrivateKey, 0, cnt)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		opPubKey, privateKey, err := rsaencryption.GenerateKeyPairPEM()
		if err != nil {
			panic(err)
		}
		pk, err := rsaencryption.PEMToPrivateKey(privateKey)
		if err != nil {
			panic(err)
		}

		nodes = append(nodes, &spectypes.Operator{
			OperatorID:        spectypes.OperatorID(i),
			SSVOperatorPubKey: opPubKey,
		})
		sks[spectypes.OperatorID(i)] = sk
		rsaKeys = append(rsaKeys, pk)
	}
	return sks, nodes, rsaKeys
}
