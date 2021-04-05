package spectesting

//func prepareInstance(t *testing.T, nodes map[uint64]*proto.Node, secretKeys map[uint64]*bls.SecretKey) *Instance {
//	return &Instance{
//		PrePrepareMessages:  msgcontinmem.New(),
//		PrepareMessages:     msgcontinmem.New(),
//		ChangeRoundMessages: msgcontinmem.New(),
//		Params: &proto.InstanceParams{
//			ConsensusParams: proto.DefaultConsensusParams(),
//			IbftCommittee:   nodes,
//		},
//		State: &proto.State{
//			Round:         1,
//			Lambda:        []byte("Lamba"),
//			PreparedRound: 0,
//			PreparedValue: nil,
//		},
//		Me: &proto.Node{
//			IbftId: 0,
//			Pk:     nodes[0].Pk,
//			Sk:     secretKeys[0].Serialize(),
//		},
//		Consensus: bytesval.New([]byte(time.Now().Weekday().String())),
//		Logger:    zaptest.NewLogger(t),
//	}
//}
