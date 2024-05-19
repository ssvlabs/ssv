package mock

import (
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
)

var _ peers.NodeInfoIndex = NodeInfoIndex{}

type NodeInfoIndex struct {
	MockNodeInfo   *records.NodeInfo
	MockSelfSealed []byte
}

func (m NodeInfoIndex) SelfSealed() ([]byte, error) {
	if len(m.MockSelfSealed) != 0 {
		return m.MockSelfSealed, nil
	} else {
		return nil, errors.New("error")
	}
}

func (m NodeInfoIndex) Self() *records.NodeInfo {
	//TODO implement me
	panic("implement me")
}

func (m NodeInfoIndex) UpdateSelfRecord(newInfo *records.NodeInfo) {
	//TODO implement me
	panic("implement me")
}

func (m NodeInfoIndex) SetNodeInfo(id peer.ID, node *records.NodeInfo) {}

func (m NodeInfoIndex) NodeInfo(id peer.ID) *records.NodeInfo {
	return m.MockNodeInfo
}
