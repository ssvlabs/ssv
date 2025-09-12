package p2p

import (
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/api"
	ssvnetwork "github.com/ssvlabs/ssv/network"
)

// Handler exposes HTTP handlers to manage pinned peers at runtime.
type Handler struct {
	provider            ssvnetwork.PinnedPeers
	checkPeerConnection peerConnectionCheckFn
}

// New creates a pinned peers HTTP handler.
func New(provider ssvnetwork.PinnedPeers, connCheckFn peerConnectionCheckFn) *Handler {
	return &Handler{
		provider:            provider,
		checkPeerConnection: connCheckFn,
	}
}

// List returns the current pinned peers.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	list := h.provider.ListPinned()
	resp := listPinnedResponse{Pinned: make([]pinnedPeerJSON, 0, len(list))}
	for _, ai := range list {
		pp := pinnedPeerJSON{ID: ai.ID.String(), Addresses: make([]string, 0, len(ai.Addrs))}
		for _, addr := range ai.Addrs {
			pp.Addresses = append(pp.Addresses, addr.String())
		}
		pp.Connected = h.checkPeerConnection(ai.ID)
		resp.Pinned = append(resp.Pinned, pp)
	}
	return api.Render(w, r, resp)
}

// Add adds one or more peers to the pinned list. Requires full multiaddrs with /p2p/<peerID>.
func (h *Handler) Add(w http.ResponseWriter, r *http.Request) error {
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
		if err := h.provider.PinPeer(*ai); err != nil {
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
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) error {
	var req unpinPeersRequest
	if err := api.Bind(r, &req); err != nil {
		return api.BadRequestError(fmt.Errorf("invalid request: %w", err))
	}
	ids, invalids := validatePeerIDs(req.Peers)
	if len(invalids) > 0 {
		return &badRequestInvalid{Message: "invalid unpin request", Invalid: invalids}
	}
	existing := make(map[string]struct{})
	for _, ai := range h.provider.ListPinned() {
		existing[ai.ID.String()] = struct{}{}
	}
	var resp unpinPeersResponse
	for _, pid := range ids {
		id := pid.String()
		if _, ok := existing[id]; !ok {
			resp.NotFound = append(resp.NotFound, id)
			continue
		}
		if err := h.provider.UnpinPeer(pid); err != nil {
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

// peerConnectionCheckFn provides a minimal, testable interface for connection checks.
type peerConnectionCheckFn func(peer.ID) bool

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
