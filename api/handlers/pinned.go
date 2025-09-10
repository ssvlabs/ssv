package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	p2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/api"
	ssvnetwork "github.com/ssvlabs/ssv/network"
)

// PinnedP2PPeers exposes HTTP handlers to manage pinned peers at runtime.
type PinnedP2PPeers struct {
	Provider ssvnetwork.PinnedPeers
	Conn     connChecker
}

type pinnedPeerJSON struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Connected bool     `json:"connected"`
}

type listPinnedResponse struct {
	Pinned []pinnedPeerJSON `json:"pinned"`
}

type pinPeersRequest struct {
	Peers []string `json:"peers"`
}

type pinPeersResponse struct {
	Added   []string     `json:"added"`
	Failed  []failedItem `json:"failed,omitempty"`
	Partial bool         `json:"partial_success,omitempty"`
}

type unpinPeersRequest struct {
	Peers []string `json:"peers"`
}

type unpinPeersResponse struct {
	Removed  []string     `json:"removed"`
	NotFound []string     `json:"not_found"`
	Failed   []failedItem `json:"failed,omitempty"`
	Partial  bool         `json:"partial_success,omitempty"`
}

type invalidItem struct {
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

type failedItem struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// badRequestInvalid is a renderer+error that returns 400 with a structured invalid list.
type badRequestInvalid struct {
	Message string        `json:"error"`
	Invalid []invalidItem `json:"invalid"`
}

func (e *badRequestInvalid) Error() string { return e.Message }

func (e *badRequestInvalid) Render(w http.ResponseWriter, r *http.Request) error {
	// Only set the status; the outer render.Render will serialize the body.
	render.Status(r, http.StatusBadRequest)
	return nil
}

// validatePeers parses and validates raw peer strings. On success returns a list
// of AddrInfo; on failure returns invalid items with reasons. No side-effects.
func validatePeers(raws []string) (valid []*peer.AddrInfo, invalid []invalidItem) {
	for _, raw := range raws {
		if len(raw) == 0 || raw[0] != '/' {
			invalid = append(invalid, invalidItem{Item: raw, Reason: "expected full multiaddr"})
			continue
		}
		ai, err := peer.AddrInfoFromString(raw)
		if err != nil || len(ai.Addrs) == 0 {
			reason := "invalid multiaddr"
			if err != nil {
				reason = err.Error()
			}
			invalid = append(invalid, invalidItem{Item: raw, Reason: reason})
			continue
		}
		valid = append(valid, ai)
	}
	return
}

// validatePeerIDs parses raw strings as libp2p peer IDs. It rejects anything that
// looks like a multiaddr (starts with '/') with a friendly message to avoid
// confusion with Add, which accepts full multiaddrs.
func validatePeerIDs(raws []string) (valid []peer.ID, invalid []invalidItem) {
	for _, raw := range raws {
		if len(raw) == 0 {
			invalid = append(invalid, invalidItem{Item: raw, Reason: "empty value"})
			continue
		}
		if raw[0] == '/' {
			invalid = append(invalid, invalidItem{Item: raw, Reason: "expected peer ID, not multiaddr"})
			continue
		}
		id, err := peer.Decode(raw)
		if err != nil {
			invalid = append(invalid, invalidItem{Item: raw, Reason: err.Error()})
			continue
		}
		valid = append(valid, id)
	}
	return
}

// List returns the current pinned peers.
func (p *PinnedP2PPeers) List(w http.ResponseWriter, r *http.Request) error {
	list := p.Provider.ListPinned()
	resp := listPinnedResponse{Pinned: make([]pinnedPeerJSON, 0, len(list))}
	for _, ai := range list {
		pp := pinnedPeerJSON{ID: ai.ID.String(), Addresses: make([]string, 0, len(ai.Addrs))}
		for _, addr := range ai.Addrs {
			pp.Addresses = append(pp.Addresses, addr.String())
		}
		pp.Connected = p.Conn.IsConnected(ai.ID)
		resp.Pinned = append(resp.Pinned, pp)
	}
	return api.Render(w, r, resp)
}

// Add adds one or more peers to the pinned list. Requires full multiaddrs with /p2p/<peerID>.
func (p *PinnedP2PPeers) Add(w http.ResponseWriter, r *http.Request) error {
	var req pinPeersRequest
	if err := api.Bind(r, &req); err != nil {
		return api.BadRequestError(fmt.Errorf("invalid request: %w", err))
	}
	valids, invalids := validatePeers(req.Peers)
	if len(invalids) > 0 {
		return &badRequestInvalid{Message: "invalid pin request", Invalid: invalids}
	}
	var resp pinPeersResponse
	for _, ai := range valids {
		id := ai.ID.String()
		if err := p.Provider.PinPeer(*ai); err != nil {
			resp.Failed = append(resp.Failed, failedItem{ID: id, Reason: err.Error()})
			continue
		}
		resp.Added = append(resp.Added, id)
	}
	if len(resp.Failed) > 0 {
		resp.Partial = true
	}
	return api.Render(w, r, resp)
}

// Remove removes peers from the pinned list. Accepts peer IDs only.
func (p *PinnedP2PPeers) Remove(w http.ResponseWriter, r *http.Request) error {
	var req unpinPeersRequest
	if err := api.Bind(r, &req); err != nil {
		return api.BadRequestError(fmt.Errorf("invalid request: %w", err))
	}
	ids, invalids := validatePeerIDs(req.Peers)
	if len(invalids) > 0 {
		return &badRequestInvalid{Message: "invalid unpin request", Invalid: invalids}
	}
	existing := make(map[string]struct{})
	for _, ai := range p.Provider.ListPinned() {
		existing[ai.ID.String()] = struct{}{}
	}
	var resp unpinPeersResponse
	for _, pid := range ids {
		id := pid.String()
		if _, ok := existing[id]; !ok {
			resp.NotFound = append(resp.NotFound, id)
			continue
		}
		if err := p.Provider.UnpinPeer(pid); err != nil {
			resp.Failed = append(resp.Failed, failedItem{ID: id, Reason: err.Error()})
			continue
		}
		resp.Removed = append(resp.Removed, id)
		delete(existing, id)
	}
	if len(resp.Failed) > 0 {
		resp.Partial = true
	}
	return api.Render(w, r, resp)
}

// connChecker provides a minimal, testable interface for connection checks.
type connChecker interface {
	IsConnected(peer.ID) bool
}

// libp2pConnChecker adapts libp2p p2pnetwork.Network to ConnChecker.
type libp2pConnChecker struct{ net p2pnetwork.Network }

func NewLibp2pConnChecker(net p2pnetwork.Network) *libp2pConnChecker {
	return &libp2pConnChecker{net: net}
}

func (c *libp2pConnChecker) IsConnected(id peer.ID) bool {
	return c.net.Connectedness(id) == p2pnetwork.Connected
}
