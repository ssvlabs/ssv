package incoming

//
//import (
//	"encoding/json"
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/ibft/sync"
//	"github.com/bloxapp/ssv/network"
//	"github.com/stretchr/testify/require"
//	"go.uber.org/zap"
//	"testing"
//)
//
//func TestTestNetwork_GetDecidedByRange(t *testing.T) {
//	sks, _ := sync.GenerateNodes(4)
//
//	decided250Seq := sync.DecidedArr(t, 250, sks, []byte("lambda"))
//
//	tests := []struct {
//		name           string
//		identifier     []byte
//		params         []uint64
//		expectedSeq    []uint64
//		expectedResL   int
//		maxBatch       int
//		decidedStorage []*proto.SignedMessage
//		errorMap       map[string]error
//		expectedError  string
//	}{
//		{
//			"fetch 0-10",
//			[]byte("lambda"),
//			[]uint64{0, 10},
//			[]uint64{0, 10},
//			11,
//			100,
//			decided250Seq,
//			nil,
//			"",
//		},
//		{
//			"fetch 0-100",
//			[]byte("lambda"),
//			[]uint64{0, 100},
//			[]uint64{0, 100},
//			101,
//			100,
//			decided250Seq,
//			nil,
//			"",
//		},
//		{
//			"fetch 0-139, should receive first 100",
//			[]byte("lambda"),
//			[]uint64{0, 139},
//			[]uint64{0, 100},
//			101,
//			100,
//			decided250Seq,
//			nil,
//			"",
//		},
//		{
//			"fetch 58-159, should receive first 100",
//			[]byte("lambda"),
//			[]uint64{58, 158},
//			[]uint64{58, 158},
//			101,
//			100,
//			decided250Seq,
//			nil,
//			"",
//		},
//		{
//			"fetch 1000-1058, should receive 0 results",
//			[]byte("lambda"),
//			[]uint64{1000, 1058},
//			[]uint64{0, 0},
//			0,
//			100,
//			decided250Seq,
//			nil,
//			"",
//		},
//		{
//			"params value just 1 - invalid",
//			[]byte("lambda"),
//			[]uint64{1000},
//			[]uint64{0, 0},
//			0,
//			100,
//			decided250Seq,
//			nil,
//			"invalid get decided request: sync msg invalid: params should contain 2 elements",
//		},
//		{
//			"params value just 0 - invalid",
//			[]byte("lambda"),
//			[]uint64{1000},
//			[]uint64{0, 0},
//			0,
//			100,
//			decided250Seq,
//			nil,
//			"invalid get decided request: sync msg invalid: params should contain 2 elements",
//		},
//		{
//			"params value 3 - invalid",
//			[]byte("lambda"),
//			[]uint64{1000, 1001, 1002},
//			[]uint64{0, 0},
//			0,
//			100,
//			decided250Seq,
//			nil,
//			"invalid get decided request: sync msg invalid: params should contain 2 elements",
//		},
//		{
//			"params value start higher then end - invalid",
//			[]byte("lambda"),
//			[]uint64{1000, 900},
//			[]uint64{0, 0},
//			0,
//			100,
//			decided250Seq,
//			nil,
//			"invalid get decided request: sync msg invalid: param[0] should be <= param[1]",
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			ibftStorage := sync.TestingIbftStorage(t)
//
//			// save decided
//			for _, d := range test.decidedStorage {
//				require.NoError(t, ibftStorage.SaveDecided(d))
//			}
//
//			s := sync.NewTestStream("")
//
//			handler := ReqHandler{
//				paginationMaxSize: uint64(test.maxBatch),
//				identifier:        test.identifier,
//				network: sync.NewTestNetwork(t, nil, test.maxBatch, nil, nil,
//					nil, nil, nil, func(streamID string) network.SyncStream {
//						return s
//					}),
//				storage: &ibftStorage,
//				logger:  zap.L(),
//			}
//
//			handler.handleGetDecidedReq(&network.SyncChanObj{
//				Msg: &network.SyncMessage{
//					SignedMessages: nil,
//					FromPeerID:     "",
//					Params:         test.params,
//					Lambda:         []byte("lambda"),
//					Type:           0,
//					Error:          "",
//				},
//				StreamID: s.ID(),
//			})
//
//			byts := <-s.C
//			res := &network.Message{}
//			require.NoError(t, json.Unmarshal(byts, res))
//			require.Len(t, res.SyncMessage.SignedMessages, test.expectedResL)
//			if test.expectedResL > 0 {
//				require.EqualValues(t, test.expectedSeq[0], res.SyncMessage.SignedMessages[0].Message.SeqNumber)
//				require.EqualValues(t, test.expectedSeq[1], res.SyncMessage.SignedMessages[test.expectedResL-1].Message.SeqNumber)
//			}
//			if len(test.expectedError) > 0 {
//				require.EqualValues(t, test.expectedError, res.SyncMessage.Error)
//			} else {
//				require.Len(t, res.SyncMessage.Error, 0)
//			}
//		})
//	}
//}
