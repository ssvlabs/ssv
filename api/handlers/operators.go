package handlers

import (
	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/message/validation"
	"net/http"
)

type OperatorPeer struct {
	PeerID     string `json:"peer_id"`
	OperatorID uint64 `json:"operator_id"`
}

type OperatorToPeerJSON struct {
	Data []*OperatorPeer `json:"data"`
}

type Operators struct {
}

func (h Operators) List(w http.ResponseWriter, r *http.Request) error {
	opt := &OperatorToPeerJSON{make([]*OperatorPeer, 0)}
	validation.PeerIDtoSignerMtx.Lock()
	defer validation.PeerIDtoSignerMtx.Unlock()
	for k, v := range validation.PeerIDtoSigner {
		opt.Data = append(opt.Data, &OperatorPeer{PeerID: k.String(), OperatorID: v})
	}
	return api.Render(w, r, opt)
}
