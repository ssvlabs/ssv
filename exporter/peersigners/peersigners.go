// Package peersigners is an EXPERIMENT (not for release): it counts, per operator ID, which
// peer first delivered each valid single-signer message signed by that operator. An operator's
// own node is usually the first deliverer of its own messages when we are in its mesh, which
// lets us map peers (and the version they advertise) to operator IDs.
package peersigners

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type recorder struct {
	mu      sync.Mutex
	started time.Time
	// counts[signer][peer] = number of valid messages signed by signer first delivered by peer.
	counts map[spectypes.OperatorID]map[peer.ID]uint64
	totals map[spectypes.OperatorID]uint64
}

var global = &recorder{
	started: time.Now(),
	counts:  make(map[spectypes.OperatorID]map[peer.ID]uint64),
	totals:  make(map[spectypes.OperatorID]uint64),
}

// Record notes that a valid message signed only by signer was first delivered by from.
func Record(signer spectypes.OperatorID, from peer.ID) {
	if from == "" {
		return
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	m := global.counts[signer]
	if m == nil {
		m = make(map[peer.ID]uint64)
		global.counts[signer] = m
	}
	m[from]++
	global.totals[signer]++
}

type peerCount struct {
	Peer  peer.ID `json:"peer"`
	Count uint64  `json:"count"`
}

type signerEntry struct {
	Signer spectypes.OperatorID `json:"signer"`
	Total  uint64               `json:"total"`
	Peers  []peerCount          `json:"peers"`
}

type response struct {
	Since   time.Time     `json:"since"`
	Signers []signerEntry `json:"signers"`
}

// Handler serves every signer's first-delivery counts per peer as JSON.
func Handler(w http.ResponseWriter, _ *http.Request) {
	global.mu.Lock()
	resp := response{Since: global.started, Signers: make([]signerEntry, 0, len(global.counts))}
	for signer, m := range global.counts {
		e := signerEntry{Signer: signer, Total: global.totals[signer], Peers: make([]peerCount, 0, len(m))}
		for p, c := range m {
			e.Peers = append(e.Peers, peerCount{Peer: p, Count: c})
		}
		resp.Signers = append(resp.Signers, e)
	}
	global.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
