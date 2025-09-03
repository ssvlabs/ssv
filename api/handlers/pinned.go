package handlers

import (
	"fmt"
	"net/http"

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
	Added         []string `json:"added"`
	AlreadyPinned []string `json:"already_pinned"`
	Invalid       []string `json:"invalid"`
}

type unpinPeersRequest struct {
	Peers []string `json:"peers"`
}

type unpinPeersResponse struct {
	Removed  []string `json:"removed"`
	NotFound []string `json:"not_found"`
	Invalid  []string `json:"invalid"`
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
	already := make(map[string]struct{})
	// Build a quick lookup of existing pins.
	for _, ai := range p.Provider.ListPinned() {
		already[ai.ID.String()] = struct{}{}
	}
	var resp pinPeersResponse
	for _, raw := range req.Peers {
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
		if err := p.Provider.PinPeer(*ai); err != nil {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		resp.Added = append(resp.Added, ai.ID.String())
		already[ai.ID.String()] = struct{}{}
	}
	return api.Render(w, r, resp)
}

// Remove removes peers from the pinned list. Requires full multiaddrs with /p2p/<peerID>.
func (p *PinnedP2PPeers) Remove(w http.ResponseWriter, r *http.Request) error {
	var req unpinPeersRequest
	if err := api.Bind(r, &req); err != nil {
		return api.BadRequestError(fmt.Errorf("invalid request: %w", err))
	}
	existing := make(map[string]struct{})
	for _, ai := range p.Provider.ListPinned() {
		existing[ai.ID.String()] = struct{}{}
	}
	var resp unpinPeersResponse
	for _, raw := range req.Peers {
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
		if err := p.Provider.UnpinPeer(id); err != nil {
			resp.Invalid = append(resp.Invalid, raw)
			continue
		}
		resp.Removed = append(resp.Removed, id.String())
		delete(existing, id.String())
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
