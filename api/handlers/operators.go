package handlers

import (
	"net/http"

	"github.com/libp2p/go-libp2p/core/peer"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/peers"
)

type OperatorPeer struct {
	PeerID     string   `json:"peer_id"`
	OperatorID []uint64 `json:"operator_id"`
	Version    string   `json:"version"`
}

type OperatorToPeerJSON struct {
	Data []*OperatorPeer `json:"data"`
}

type Operators struct {
	PeersIndex peers.Index
}

func (h Operators) List(w http.ResponseWriter, r *http.Request) error {
	opt := &OperatorToPeerJSON{make([]*OperatorPeer, 0)}

	validation.PeerIDtoSigner.Range(func(k peer.ID, v []spectypes.OperatorID) bool {
		opdata := &OperatorPeer{PeerID: k.String(), OperatorID: v}
		nodeInfo := h.PeersIndex.NodeInfo(k)
		if nodeInfo != nil && nodeInfo.Metadata != nil {
			opdata.Version = nodeInfo.Metadata.NodeVersion
		}
		opt.Data = append(opt.Data, opdata)
		return true
	})

	return api.Render(w, r, opt)
}
