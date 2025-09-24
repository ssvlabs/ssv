package p2p

import (
	"bytes"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/api"
)

type mockPinnedProvider struct {
	pinned     []peer.AddrInfo
	pinCalls   []peer.AddrInfo
	unpinCalls []peer.ID
	// optional errors by peer id
	pinErrFor   map[peer.ID]error
	unpinErrFor map[peer.ID]error
}

func (m *mockPinnedProvider) ListPinned() []peer.AddrInfo {
	out := make([]peer.AddrInfo, len(m.pinned))
	copy(out, m.pinned)
	return out
}

func (m *mockPinnedProvider) PinPeer(ai peer.AddrInfo) error {
	m.pinCalls = append(m.pinCalls, ai)
	if m.pinErrFor != nil {
		if err, ok := m.pinErrFor[ai.ID]; ok {
			return err
		}
	}
	// emulate pin success by adding to pinned list if not already
	for _, p := range m.pinned {
		if p.ID == ai.ID {
			return nil
		}
	}
	m.pinned = append(m.pinned, ai)
	return nil
}

func (m *mockPinnedProvider) UnpinPeer(id peer.ID) error {
	m.unpinCalls = append(m.unpinCalls, id)
	if m.unpinErrFor != nil {
		if err, ok := m.unpinErrFor[id]; ok {
			return err
		}
	}
	// emulate removal
	kept := make([]peer.AddrInfo, 0, len(m.pinned))
	for _, p := range m.pinned {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	m.pinned = kept
	return nil
}

// helper to generate a libp2p peer ID
func genPeerID(t *testing.T) peer.ID {
	t.Helper()
	sk, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)
	return id
}

// idsAndAddrs returns n IDs and their corresponding multiaddrs.
func idsAndAddrs(t *testing.T, n int) ([]peer.ID, []ma.Multiaddr) {
	t.Helper()
	ids := make([]peer.ID, n)
	addrs := make([]ma.Multiaddr, n)
	for i := 0; i < n; i++ {
		ids[i] = genPeerID(t)
		a, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/13001/p2p/%s", ids[i].String()))
		require.NoError(t, err)
		addrs[i] = a
	}
	return ids, addrs
}

// test helpers to reduce duplication
func newPinnedHandler(prov *mockPinnedProvider) *Handler {
	return New(prov, func(id peer.ID) bool {
		return false
	})
}

func doJSON(t *testing.T, method string, handler api.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/v1/node/pinned-peers", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	api.Handler(handler).ServeHTTP(rr, req)
	return rr
}

// callIDs returns the string IDs that were pinned in order of calls.
func callIDs(calls []peer.AddrInfo) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.ID.String())
	}
	return out
}

func TestPinned_List(t *testing.T) {
	ids, addrs := idsAndAddrs(t, 2)

	prov := &mockPinnedProvider{
		pinned: []peer.AddrInfo{
			{ID: ids[0], Addrs: []ma.Multiaddr{addrs[0]}},
			{ID: ids[1]},
		},
	}
	h := New(prov, func(id peer.ID) bool {
		return id == ids[0]
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/node/pinned-peers", nil)
	api.Handler(h.List).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp listPinnedResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Pinned, 2)
	// assert connected flags and addresses
	require.Equal(t, resp.Pinned[0].ID, ids[0].String())
	require.True(t, resp.Pinned[0].Connected)
	require.GreaterOrEqual(t, len(resp.Pinned[0].Addresses), 1)
	require.Equal(t, ids[1].String(), resp.Pinned[1].ID)
	require.False(t, resp.Pinned[1].Connected)
}

func TestPinned_Add_BestEffort(t *testing.T) {
	ids, addrs := idsAndAddrs(t, 3)

	prov := &mockPinnedProvider{
		pinned: []peer.AddrInfo{{ID: ids[0], Addrs: []ma.Multiaddr{addrs[0]}}},
		pinErrFor: map[peer.ID]error{
			ids[2]: errors.New("pin failed"),
		},
	}
	h := newPinnedHandler(prov)

	// Provide: addr2 (valid add), addr1 (already pinned), addr3 (pin will fail)
	body := pinPeersRequest{Peers: []string{addrs[1].String(), addrs[0].String(), addrs[2].String()}}
	rr := doJSON(t, http.MethodPost, h.Add, body)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp pinPeersResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	require.Contains(t, resp.Added, ids[1].String())
	// id[0] is already pinned; Add merges addrs and reports success
	require.Contains(t, resp.Added, ids[0].String())
	// failed should contain id3, partial success is true
	require.Len(t, resp.Failed, 1)
	require.Equal(t, ids[2].String(), resp.Failed[0].ID)
	require.True(t, resp.Partial)

	// Pin calls should include id1, id2 and id3; id1 merges addrs
	called := callIDs(prov.pinCalls)
	require.Contains(t, called, ids[0].String())
	require.Contains(t, called, ids[1].String())
	require.Contains(t, called, ids[2].String())
}

func TestPinned_Add_400OnInvalid(t *testing.T) {
	_, addrs := idsAndAddrs(t, 1)

	prov := &mockPinnedProvider{}
	h := newPinnedHandler(prov)

	// Include a malformed multiaddr among inputs
	body := pinPeersRequest{Peers: []string{addrs[0].String(), "/ip4/127.0.0.1/tcp/13001"}}
	rr := doJSON(t, http.MethodPost, h.Add, body)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	// ensure no pin calls were performed
	require.Empty(t, prov.pinCalls)
}

func TestPinned_Remove_BestEffort(t *testing.T) {
	ids, addrs := idsAndAddrs(t, 2)

	prov := &mockPinnedProvider{
		pinned: []peer.AddrInfo{{ID: ids[0], Addrs: []ma.Multiaddr{addrs[0]}}, {ID: ids[1]}},
	}
	h := newPinnedHandler(prov)

	// Valid removals only
	body := unpinPeersRequest{Peers: []string{ids[1].String(), ids[0].String()}}
	rr := doJSON(t, http.MethodDelete, h.Remove, body)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp unpinPeersResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	// both ids[0] and ids[1] removed, no failures
	require.ElementsMatch(t, []string{ids[0].String(), ids[1].String()}, resp.Removed)
	require.Empty(t, resp.Failed)
	require.False(t, resp.Partial)

	// Verify UnpinPeer calls
	called := make(map[string]struct{}, len(prov.unpinCalls))
	for _, id := range prov.unpinCalls {
		called[id.String()] = struct{}{}
	}
	require.Contains(t, called, ids[0].String())
	require.Contains(t, called, ids[1].String())
}

func TestPinned_Remove_400OnInvalid(t *testing.T) {
	ids, addrs := idsAndAddrs(t, 1)

	prov := &mockPinnedProvider{pinned: []peer.AddrInfo{{ID: ids[0], Addrs: []ma.Multiaddr{addrs[0]}}}}
	h := newPinnedHandler(prov)

	// Mixed valid peer ID and multiaddr should 400 with friendly message
	body := unpinPeersRequest{Peers: []string{ids[0].String(), "/ip4/127.0.0.1/tcp/13001"}}
	rr := doJSON(t, http.MethodDelete, h.Remove, body)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Empty(t, prov.unpinCalls)
}

func TestPinned_Remove_FailedBestEffort(t *testing.T) {
	ids, addrs := idsAndAddrs(t, 2)

	prov := &mockPinnedProvider{
		pinned:      []peer.AddrInfo{{ID: ids[0], Addrs: []ma.Multiaddr{addrs[0]}}, {ID: ids[1]}},
		unpinErrFor: map[peer.ID]error{ids[1]: errors.New("unpin failed")},
	}
	h := newPinnedHandler(prov)

	body := unpinPeersRequest{Peers: []string{ids[1].String(), ids[0].String()}}
	rr := doJSON(t, http.MethodDelete, h.Remove, body)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp unpinPeersResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Contains(t, resp.Removed, ids[0].String())
	require.Len(t, resp.Failed, 1)
	require.Equal(t, ids[1].String(), resp.Failed[0].ID)
	require.True(t, resp.Partial)
}
