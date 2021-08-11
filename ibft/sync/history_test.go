package sync

import (
	"github.com/bloxapp/ssv/ibft/proto"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

// generateNodes generates randomly nodes
func generateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func decidedArr(t *testing.T, maxSeq uint64, sks map[uint64]*bls.SecretKey) []*proto.SignedMessage {
	ret := make([]*proto.SignedMessage, 0)
	for i := uint64(0); i <= maxSeq; i++ {
		ret = append(ret, multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
			Type:      proto.RoundState_Decided,
			Round:     1,
			Lambda:    []byte("lambda"),
			SeqNumber: i,
		}))
	}
	return ret
}

func multiSignMsg(t *testing.T, ids []uint64, sks map[uint64]*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

	var agg *bls.Sign
	for _, id := range ids {
		signature, err := msg.Sign(sks[id])
		require.NoError(t, err)
		if agg == nil {
			agg = signature
		} else {
			agg.Add(signature)
		}
	}

	return &proto.SignedMessage{
		Message:   msg,
		Signature: agg.Serialize(),
		SignerIds: ids,
	}
}

func ibftStorage(t *testing.T) collections.IbftStorage {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	})
	require.NoError(t, err)
	return collections.NewIbft(db, zap.L(), "attestation")
}

func TestSync(t *testing.T) {
	sks, _ := generateNodes(4)
	decided0 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 0,
	})
	decided1 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 1,
	})
	decided2 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 2,
	})
	decided250Seq := decidedArr(t, 250, sks)

	tests := []struct {
		name               string
		valdiatorPK        []byte
		identifier         []byte
		peers              []string
		highestMap         map[string]*proto.SignedMessage
		decidedArrMap      map[string][]*proto.SignedMessage
		errorMap           map[string]error
		expectedHighestSeq int64
		expectedError      string
	}{
		{
			"sync to seq 0",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided0,
				"3": decided0,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0},
				"3": {decided0},
			},
			nil,
			0,
			"",
		},
		{
			"sync to seq 250",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided250Seq[len(decided250Seq)-1],
				"3": decided250Seq[len(decided250Seq)-1],
			},
			map[string][]*proto.SignedMessage{
				"2": decided250Seq,
				"3": decided250Seq,
			},
			nil,
			250,
			"",
		},
		{
			"sync to seq 1",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided1,
				"3": decided1,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1},
				"3": {decided0, decided1},
			},
			nil,
			1,
			"",
		},
		{
			"sync to seq 2",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1, decided2},
				"3": {decided0, decided1, decided2},
			},
			nil,
			2,
			"",
		},
		{
			"try syncing to seq 2 with missing the last, error on history fetch",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1},
				"3": {decided0, decided1},
			},
			nil,
			2,
			"could not fetch decided by range during sync: returned decided by range messages miss sequence number 2",
		},
		{
			"try syncing to seq 2 with missing in middle, error on history fetch",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided2},
				"3": {decided0, decided2},
			},
			nil,
			2,
			"could not fetch decided by range during sync: returned decided by range messages miss sequence number 1",
		},
		{
			"try syncing to seq 2 with missing in start, error on history fetch",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided1, decided2},
				"3": {decided1, decided2},
			},
			nil,
			2,
			"could not fetch decided by range during sync: returned decided by range messages miss sequence number 0",
		},
		{
			"try syncing with differ decided peers",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided1,
				"3": decided0,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1},
				"3": {decided0},
			},
			nil,
			1,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := ibftStorage(t)
			s := NewHistorySync(zap.L(), test.valdiatorPK, test.identifier, newTestNetwork(t, test.peers, 100, test.highestMap, test.errorMap, test.decidedArrMap, nil), &storage, func(msg *proto.SignedMessage) error {
				return nil
			})
			err := s.Start()

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				if test.expectedHighestSeq == -1 {
					//require.Nil(t, res)
				} else {
					// verify saved instances
					for i := int64(0); i <= test.expectedHighestSeq; i++ {
						decided, err := storage.GetDecided(test.identifier, uint64(i))
						require.NoError(t, err)
						require.EqualValues(t, uint64(i), decided.Message.SeqNumber)
					}
					// verify saved highest
					highest, err := storage.GetHighestDecidedInstance(test.identifier)
					require.NoError(t, err)
					require.EqualValues(t, test.expectedHighestSeq, highest.Message.SeqNumber)
				}
			}
		})
	}
}
