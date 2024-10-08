package handlers

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/peers"
	"net/http"
)

type OperatorPeer struct {
	PeerID     string `json:"peer_id"`
	OperatorID uint64 `json:"operator_id"`
	Version    string `json:"version"`
}

type OperatorToPeerJSON struct {
	Data []*OperatorPeer `json:"data"`
}

type Operators struct {
	PeersIndex peers.Index
}

func (h Operators) List(w http.ResponseWriter, r *http.Request) error {
	opt := &OperatorToPeerJSON{make([]*OperatorPeer, 0)}

	mapcopy := make(map[peer.ID]uint64)
	validation.PeerIDtoSignerMtx.Lock()
	for k, v := range validation.PeerIDtoSigner {
		mapcopy[k] = v
	}
	validation.PeerIDtoSignerMtx.Unlock()

	for k, v := range mapcopy {
		opdata := &OperatorPeer{PeerID: k.String(), OperatorID: v}
		nodeInfo := h.PeersIndex.NodeInfo(k)
		if nodeInfo != nil && nodeInfo.Metadata != nil {
			opdata.Version = nodeInfo.Metadata.NodeVersion
		}
		opt.Data = append(opt.Data, opdata)
	}
	return api.Render(w, r, opt)
}
