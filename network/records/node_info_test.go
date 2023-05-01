package records

//func TestNodeInfo_Seal_Consume(t *testing.T) {
//	netKey, _, err := libp2pcrypto.GenerateSecp256k1Key(rand.Reader)
//	require.NoError(t, err)
//	ni := &NodeInfo{
//		ForkVersion: forksprotocol.GenesisForkVersion,
//		NetworkID:   "testnet",
//		Metadata: &NodeMetadata{
//			NodeVersion:   "v0.1.12",
//			ExecutionNode: "geth/x",
//			ConsensusNode: "prysm/x",
//			OperatorID:    "xxx",
//		},
//	}
//
//	data, err := ni.Seal(netKey, HandshakeData{}, nil)
//	require.NoError(t, err)
//	parsedRec := &NodeInfo{}
//	require.NoError(t, parsedRec.Consume(data))
//
//	require.Equal(t, ni.ForkVersion, parsedRec.ForkVersion)
//	require.Equal(t, ni.NetworkID, parsedRec.NetworkID)
//	require.Equal(t, ni.Metadata.NodeVersion, parsedRec.Metadata.NodeVersion)
//	require.Equal(t, ni.Metadata.ExecutionNode, parsedRec.Metadata.ExecutionNode)
//	require.Equal(t, ni.Metadata.ConsensusNode, parsedRec.Metadata.ConsensusNode)
//	require.Equal(t, ni.Metadata.OperatorID, parsedRec.Metadata.OperatorID)
//}
