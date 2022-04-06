package signed_msg

//
//func TestBasicMsgValidation(t *testing.T) {
//	pipeline := BasicMsgValidation()
//
//	err := pipeline.Run(nil)
//	require.EqualError(t, err, "signed message is nil")
//
//	err = pipeline.Run(&proto.SignedMessage{})
//	require.EqualError(t, err, "message body is nil")
//
//	err = pipeline.Run(&proto.SignedMessage{
//		Message: &proto.Message{},
//	})
//	require.NoError(t, err)
//}
