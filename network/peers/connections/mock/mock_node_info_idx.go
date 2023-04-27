package mock

import (
	"crypto/rsa"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

var _ peers.NodeInfoIndex = NodeInfoIndex{}

type NodeInfoIndex struct {
	MockNodeInfo   *records.NodeInfo
	MockSelfSealed []byte
}

func (m NodeInfoIndex) SelfSealed(sender, recipient peer.ID, operatorPrivateKey *rsa.PrivateKey) ([]byte, error) {
	return m.MockSelfSealed, nil
}

func (m NodeInfoIndex) Self() *records.NodeInfo {
	//TODO implement me
	panic("implement me")
}

func (m NodeInfoIndex) UpdateSelfRecord(newInfo *records.NodeInfo) {
	//TODO implement me
	panic("implement me")
}

func (m NodeInfoIndex) AddNodeInfo(logger *zap.Logger, id peer.ID, node *records.NodeInfo) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeInfoIndex) GetNodeInfo(id peer.ID) (*records.NodeInfo, error) {
	return m.MockNodeInfo, nil
}
