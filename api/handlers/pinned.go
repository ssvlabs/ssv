package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/api"
)

// PinnedPeersProvider is the minimal interface the P2P layer must implement for pinned peers management.
type PinnedPeersProvider interface {
	ListPinned() []peer.AddrInfo
	PinPeer(peer.AddrInfo) error
	UnpinPeer(peer.ID) error
}

// PinnedP2PPeers exposes HTTP handlers to manage pinned peers at runtime.
type PinnedP2PPeers struct {
	Provider PinnedPeersProvider
	Conn     ConnChecker
}

type PinnedPeerJSON struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Connected bool     `json:"connected"`
}

type ListPinnedResponse struct {
	Pinned []PinnedPeerJSON `json:"pinned"`
}

type PinPeersRequest struct {
	Peers []string `json:"peers"`
}

type PinPeersResponse struct {
	Added         []string `json:"added"`
	AlreadyPinned []string `json:"already_pinned"`
	Invalid       []string `json:"invalid"`
}

type UnpinPeersRequest struct {
	Peers []string `json:"peers"`
}

type UnpinPeersResponse struct {
	Removed  []string `json:"removed"`
	NotFound []string `json:"not_found"`
	Invalid  []string `json:"invalid"`
}

// List returns the current pinned peers.
func (h *PinnedP2PPeers) List(w http.ResponseWriter, r *http.Request) error {
	list := h.Provider.ListPinned()
	resp := ListPinnedResponse{Pinned: make([]PinnedPeerJSON, 0, len(list))}
	for _, ai := range list {
		pp := PinnedPeerJSON{ID: ai.ID.String()}
		for _, addr := range ai.Addrs {
			pp.Addresses = append(pp.Addresses, addr.String())
		}
		pp.Connected = h.Conn.IsConnected(ai.ID)
		resp.Pinned = append(resp.Pinned, pp)
	}
	return json.NewEncoder(w).Encode(resp)
}

// Add adds one or more peers to the pinned list. Requires full multiaddrs with /p2p/<peerID>.
func (h *PinnedP2PPeers) Add(w http.ResponseWriter, r *http.Request) error {
	var req PinPeersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return api.BadRequestError(fmt.Errorf("invalid request: %w", err))
	}
	already := make(map[string]struct{})
	// Build a quick lookup of existing pins.
	for _, ai := range h.Provider.ListPinned() {
		already[ai.ID.String()] = struct{}{}
	}
	var resp PinPeersResponse
	for _, raw := range req.Peers {
		if raw == "" {
			continue
		}
		// Require full multiaddr with address for pinned peers; reject bare IDs.
		if len(raw) == 0 || raw[0] != '/' {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		ai, err := peer.AddrInfoFromString(raw)
		if err != nil || len(ai.Addrs) == 0 {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		if _, ok := already[ai.ID.String()]; ok {
			resp.AlreadyPinned = append(resp.AlreadyPinned, ai.ID.String())
			continue
		}
		if err := h.Provider.PinPeer(*ai); err != nil {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		resp.Added = append(resp.Added, ai.ID.String())
		already[ai.ID.String()] = struct{}{}
	}
	return json.NewEncoder(w).Encode(resp)
}

// Remove removes peers from the pinned list. Requires full multiaddrs with /p2p/<peerID>.
func (h *PinnedP2PPeers) Remove(w http.ResponseWriter, r *http.Request) error {
	var req UnpinPeersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return api.BadRequestError(fmt.Errorf("invalid request: %w", err))
	}
	existing := make(map[string]struct{})
	for _, ai := range h.Provider.ListPinned() {
		existing[ai.ID.String()] = struct{}{}
	}
	var resp UnpinPeersResponse
	for _, raw := range req.Peers {
		if raw == "" {
			continue
		}
		// Require full multiaddr and extract ID; reject bare IDs.
		if len(raw) == 0 || raw[0] != '/' {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		ai, err := peer.AddrInfoFromString(raw)
		if err != nil || ai.ID == "" {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		id := ai.ID
		if _, ok := existing[id.String()]; !ok {
			resp.NotFound = append(resp.NotFound, id.String())
			continue
		}
		if err := h.Provider.UnpinPeer(id); err != nil {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		resp.Removed = append(resp.Removed, id.String())
		delete(existing, id.String())
	}
	return json.NewEncoder(w).Encode(resp)
}

// ConnChecker provides a minimal, testable interface for connection checks.
type ConnChecker interface {
	IsConnected(peer.ID) bool
}

// Libp2pConnChecker adapts libp2p network.Network to ConnChecker.
type Libp2pConnChecker struct{ net network.Network }

func NewLibp2pConnChecker(net network.Network) *Libp2pConnChecker {
	return &Libp2pConnChecker{net: net}
}

func (c *Libp2pConnChecker) IsConnected(id peer.ID) bool {
	return c.net.Connectedness(id) == network.Connected
}
