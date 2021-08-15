package history

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestSync(t *testing.T) {
	sks, _ := sync.GenerateNodes(4)
	decided0 := sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 0,
	})
	decided1 := sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 1,
	})
	decided2 := sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 2,
	})
	decided250Seq := sync.DecidedArr(t, 250, sks)

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
			storage := sync.IbftStorage(t)
			s := New(zap.L(), test.valdiatorPK, test.identifier, sync.NewTestNetwork(t, test.peers, 100, test.highestMap, test.errorMap, test.decidedArrMap, nil), &storage, func(msg *proto.SignedMessage) error {
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
