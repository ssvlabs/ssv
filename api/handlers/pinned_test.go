package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	crand "crypto/rand"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/api"
)

// mockPinnedProvider implements PinnedPeersProvider for testing.
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

// mockConnChecker implements ConnChecker for testing.
type mockConnChecker struct{ connected map[peer.ID]bool }

func (m *mockConnChecker) IsConnected(id peer.ID) bool { return m.connected[id] }

// helper to generate a libp2p peer ID
func genPeerID(t *testing.T) peer.ID {
	t.Helper()
	sk, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)
	return id
}

func maFor(id peer.ID) string {
	return fmt.Sprintf("/ip4/127.0.0.1/tcp/13001/p2p/%s", id.String())
}

func TestPinned_List(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)

	addr1, _ := ma.NewMultiaddr(maFor(id1))

	prov := &mockPinnedProvider{
		pinned: []peer.AddrInfo{
			{ID: id1, Addrs: []ma.Multiaddr{addr1}},
			{ID: id2},
		},
	}
	conn := &mockConnChecker{connected: map[peer.ID]bool{id1: true, id2: false}}

	h := &PinnedP2PPeers{Provider: prov, Conn: conn}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/node/pinned-peers", nil)
	api.Handler(h.List).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp ListPinnedResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Pinned, 2)
	// assert connected flags and addresses
	if resp.Pinned[0].ID == id1.String() {
		require.True(t, resp.Pinned[0].Connected)
		require.GreaterOrEqual(t, len(resp.Pinned[0].Addresses), 1)
		require.Equal(t, id2.String(), resp.Pinned[1].ID)
		require.False(t, resp.Pinned[1].Connected)
	} else {
		require.True(t, resp.Pinned[1].Connected)
		require.GreaterOrEqual(t, len(resp.Pinned[1].Addresses), 1)
		require.Equal(t, id2.String(), resp.Pinned[0].ID)
		require.False(t, resp.Pinned[0].Connected)
	}
}

func TestPinned_Add(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	id3 := genPeerID(t)

	addr1, _ := ma.NewMultiaddr(maFor(id1))
	addr2, _ := ma.NewMultiaddr(maFor(id2))
	addr3, _ := ma.NewMultiaddr(maFor(id3))

	prov := &mockPinnedProvider{
		pinned: []peer.AddrInfo{{ID: id1, Addrs: []ma.Multiaddr{addr1}}},
		pinErrFor: map[peer.ID]error{
			id3: errors.New("pin failed"),
		},
	}
	h := &PinnedP2PPeers{Provider: prov, Conn: &mockConnChecker{connected: map[peer.ID]bool{}}}

	// Provide: addr2 (valid add), addr1 (already pinned), addr3 (pin will fail), malformed multiaddr without /p2p
	body := PinPeersRequest{Peers: []string{addr2.String(), addr1.String(), addr3.String(), "/ip4/127.0.0.1/tcp/13001"}}
	b, _ := json.Marshal(body)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/node/pinned-peers", bytes.NewReader(b))
	api.Handler(h.Add).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp PinPeersResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	require.Contains(t, resp.Added, id2.String())
	require.Contains(t, resp.AlreadyPinned, id1.String())
	// invalids: id3 (pin failed), plus malformed multiaddr without /p2p
	require.GreaterOrEqual(t, len(resp.Invalid), 2)

	// Pin calls should include id2 and id3, not id1 (already pinned)
	called := make(map[string]bool)
	for _, c := range prov.pinCalls {
		called[c.ID.String()] = true
	}
	require.True(t, called[id2.String()])
	require.True(t, called[id3.String()])
	require.False(t, called[id1.String()])
}

func TestPinned_Remove(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	addr1, _ := ma.NewMultiaddr(maFor(id1))

	prov := &mockPinnedProvider{
		pinned: []peer.AddrInfo{{ID: id1, Addrs: []ma.Multiaddr{addr1}}, {ID: id2}},
	}
	h := &PinnedP2PPeers{Provider: prov, Conn: &mockConnChecker{connected: map[peer.ID]bool{}}}

	// Only full multiaddrs are accepted for removal
	body := UnpinPeersRequest{Peers: []string{maFor(id2), maFor(id1), "/ip4/127.0.0.1/tcp/13001"}}
	b, _ := json.Marshal(body)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/node/pinned-peers", bytes.NewReader(b))
	api.Handler(h.Remove).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp UnpinPeersResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	// both id1 and id2 removed
	require.ElementsMatch(t, []string{id1.String(), id2.String()}, resp.Removed)
	// one invalid (malformed multiaddr without /p2p), not_found should be empty since both existed
	require.Len(t, resp.Invalid, 1)
	require.Empty(t, resp.NotFound)

	// Verify UnpinPeer calls
	called := make(map[string]bool)
	for _, id := range prov.unpinCalls {
		called[id.String()] = true
	}
	require.True(t, called[id1.String()])
	require.True(t, called[id2.String()])
}
