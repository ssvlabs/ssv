package spectesting

//// IBFT ALGORITHM 2: Happy flow - a normal case operation
//func TestUponPrePrepareMessagesBroadcastsPrepare(t *testing.T) {
//	secretKeys, nodes := GenerateNodes(4)
//	instance := prepareInstance(t, nodes, secretKeys)
//
//	// Upon receiving valid PRE-PREPARE messages - 1, 2, 3
//	message := setupMessage(1, secretKeys[1], proto.RoundState_PrePrepare)
//	instance.PrePrepareMessages.AddMessage(message)
//
//	message = setupMessage(1, secretKeys[2], proto.RoundState_PrePrepare)
//	instance.PrePrepareMessages.AddMessage(message)
//
//	message = setupMessage(1, secretKeys[3], proto.RoundState_PrePrepare)
//	instance.PrePrepareMessages.AddMessage(message)
//
//	require.NoError(t, instance.UponPrePrepareMsg().Run(message))
//
//	// ...such that JUSTIFY PREPARE is true
//	res, err := instance.JustifyPrePrepare(1)
//	require.NoError(t, err)
//	require.True(t, res)
//
//	// broadcasts PREPARE message
//	prepareMessage := setupMessage(1, secretKeys[3], proto.RoundState_Prepare)
//	instance.PrepareMessages.AddMessage(prepareMessage)
//}

//func TestRoundRobinLeaderChange(t *testing.T) {
//	// should bump between instances
//	// should bump between round changes
//	t.Fail()
//}

//func setupMessage(id uint64, secretKey *bls.SecretKey, roundState proto.RoundState) *proto.SignedMessage {
//	return SignMsg(id, secretKey, &proto.Message{
//		Type:   roundState,
//		Round:  1,
//		Lambda: []byte("Lambda"),
//		Value:  []byte(time.Now().Weekday().String()),
//	})
//}
