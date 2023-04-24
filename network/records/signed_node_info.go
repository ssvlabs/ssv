package records

type SignedNodeInfo struct {
	*NodeInfo
	HandshakeData HandshakeData
	Signature     []byte
}
