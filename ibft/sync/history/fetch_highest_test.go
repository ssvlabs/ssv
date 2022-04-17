package history

//
//import (
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/ibft/sync"
//	"github.com/bloxapp/ssv/network"
//	"github.com/pkg/errors"
//	"github.com/stretchr/testify/require"
//	"go.uber.org/zap"
//	"testing"
//)
//
//func TestFindHighest(t *testing.T) {
//	sks, _ := sync.GenerateNodes(4)
//	highest1 := sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
//		Type:      proto.RoundState_Decided,
//		Round:     1,
//		Lambda:    []byte("lambda"),
//		SeqNumber: 1,
//	})
//	highest2 := sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
//		Type:      proto.RoundState_Decided,
//		Round:     1,
//		Lambda:    []byte("lambda"),
//		SeqNumber: 1,
//	})
//	highestInvalid := sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
//		Type:      proto.RoundState_Decided,
//		Round:     1,
//		Lambda:    []byte("lambda"),
//		SeqNumber: 1,
//	})
//	highestInvalid.Signature = []byte{1}
//
//	tests := []struct {
//		name               string
//		valdiatorPK        []byte
//		identifier         []byte
//		peers              []string
//		highestMap         map[string]*proto.SignedMessage
//		errorMap           map[string]error
//		expectedHighestSeq int64
//		expectedError      string
//		validateMsg        func(msg *proto.SignedMessage) error
//	}{
//		{
//			"valid",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"2"},
//			map[string]*proto.SignedMessage{
//				"2": highest1,
//			},
//			nil,
//			1,
//			"",
//			nil,
//		},
//		{
//			"all responses are empty",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"1", "2"},
//			map[string]*proto.SignedMessage{
//				"1": nil,
//				"2": nil,
//			},
//			nil,
//			-1,
//			"",
//			nil,
//		},
//		{
//			"all errors",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"1", "2"},
//			map[string]*proto.SignedMessage{},
//			map[string]error{
//				"1": errors.New("error"),
//				"2": errors.New("error"),
//			},
//			-1,
//			"could not fetch highest decided from peers",
//			nil,
//		},
//		{
//			"all invalid",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"1"},
//			map[string]*proto.SignedMessage{
//				"1": highestInvalid,
//			},
//			map[string]error{},
//			-1,
//			"could not fetch highest decided from peers",
//			func(msg *proto.SignedMessage) error {
//				return errors.New("invalid")
//			},
//		},
//		{
//			"some errors, some empty",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"1", "2"},
//			map[string]*proto.SignedMessage{
//				"2": nil,
//			},
//			map[string]error{
//				"1": errors.New("error"),
//			},
//			-1,
//			"",
//			nil,
//		},
//		{
//			"some invalid, some empty",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"1", "2"},
//			map[string]*proto.SignedMessage{
//				"1": highestInvalid,
//				"2": nil,
//			},
//			map[string]error{},
//			-1,
//			"",
//			func(msg *proto.SignedMessage) error {
//				return errors.New("invalid")
//			},
//		},
//		{
//			"some errors, some valid",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"1", "2"},
//			map[string]*proto.SignedMessage{
//				"2": highest1,
//			},
//			map[string]error{
//				"1": errors.New("error"),
//			},
//			1,
//			"",
//			nil,
//		},
//		{
//			"valid multi responses",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"2", "3"},
//			map[string]*proto.SignedMessage{
//				"2": highest1,
//				"3": highest2,
//			},
//			nil,
//			1,
//			"",
//			nil,
//		},
//		{
//			"valid multi responses different seq",
//			[]byte{1, 2, 3, 4},
//			[]byte("lambda"),
//			[]string{"2", "3"},
//			map[string]*proto.SignedMessage{
//				"2": highest1,
//				"3": sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
//					Type:      proto.RoundState_Decided,
//					Round:     1,
//					Lambda:    []byte("lambda"),
//					SeqNumber: 10,
//				}),
//			},
//			nil,
//			10,
//			"",
//			nil,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			if test.validateMsg == nil {
//				test.validateMsg = func(msg *proto.SignedMessage) error {
//					return nil
//				}
//			}
//			s := New(zap.L(), test.valdiatorPK, 4, test.identifier, sync.NewTestNetwork(t, test.peers, 100,
//				test.highestMap, test.errorMap, nil, nil, nil, func(s string) network.SyncStream {
//					return nil
//				}), nil, test.validateMsg)
//			res, _, err := s.findHighestInstance()
//
//			if len(test.expectedError) > 0 {
//				require.EqualError(t, err, test.expectedError)
//			} else {
//				require.NoError(t, err)
//				if test.expectedHighestSeq == -1 {
//					require.Nil(t, res)
//				} else {
//					require.EqualValues(t, test.expectedHighestSeq, res.Message.SeqNumber)
//				}
//			}
//		})
//	}
//}
