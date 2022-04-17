package incoming

//
//func TestTestNetwork_GetLatestChangeRound(t *testing.T) {
//	sks, _ := sync.GenerateNodes(4)
//
//	tests := []struct {
//		name          string
//		identifier    []byte
//		params        []uint64
//		latestMsg     *proto.SignedMessage
//		latestSeq     int64
//		expectedError string
//	}{
//		{
//			"fetch seq 0",
//			[]byte("lambda"),
//			[]uint64{0},
//			sync.MultiSignMsg(t, []uint64{1}, sks, &proto.Message{
//				SeqNumber: 0,
//			}),
//			0,
//			"",
//		},
//		{
//			"fetch seq 10",
//			[]byte("lambda"),
//			[]uint64{10},
//			sync.MultiSignMsg(t, []uint64{1}, sks, &proto.Message{
//				SeqNumber: 10,
//			}),
//			10,
//			"",
//		},
//		{
//			"no current instance - seq is negative",
//			[]byte("lambda"),
//			[]uint64{10},
//			nil,
//			-1,
//			"EntryNotFoundError",
//		},
//		{
//			"no current instance - wrong seq number",
//			[]byte("lambda"),
//			[]uint64{10},
//			sync.MultiSignMsg(t, []uint64{1}, sks, &proto.Message{
//				SeqNumber: 5,
//			}),
//			5,
//			"EntryNotFoundError",
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			ibftStorage := sync.TestingIbftStorage(t)
//
//			s := sync.NewTestStream("")
//
//			handler := ReqHandler{
//				paginationMaxSize: uint64(20),
//				identifier:        test.identifier,
//				network: sync.NewTestNetwork(t, nil, 20, nil,
//					nil, nil, nil, nil, func(streamID string) network.SyncStream {
//						return s
//					}),
//				storage:            &ibftStorage,
//				logger:             zap.L(),
//				seqNumber:          test.latestSeq,
//				lastChangeRoundMsg: test.latestMsg,
//			}
//
//			handler.handleGetLatestChangeRoundReq(&network.SyncChanObj{
//				Msg: &network.SyncMessage{
//					Params: test.params,
//				},
//				StreamID: s.ID(),
//			})
//
//			byts := <-s.C
//			res := &network.Message{}
//			require.NoError(t, json.Unmarshal(byts, res))
//			if len(test.expectedError) > 0 {
//				require.EqualValues(t, test.expectedError, res.SyncMessage.Error)
//			} else {
//				require.Len(t, res.SyncMessage.Error, 0)
//			}
//		})
//	}
//}
