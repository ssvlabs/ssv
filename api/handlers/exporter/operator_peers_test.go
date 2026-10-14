package exporter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter/attribution"
	"github.com/ssvlabs/ssv/network/records"
)

type fakeEvidenceSource struct {
	evidence []attribution.OperatorEvidence
	gotOps   []spectypes.OperatorID
}

func (f *fakeEvidenceSource) Snapshot(ops []spectypes.OperatorID) []attribution.OperatorEvidence {
	f.gotOps = ops
	return f.evidence
}

func (f *fakeEvidenceSource) WindowDuration() time.Duration { return time.Hour }

type fakePeerInfo map[peer.ID]struct {
	version   string
	connected bool
}

func (f fakePeerInfo) PeerInfo(id peer.ID) (string, bool) {
	info := f[id]
	return info.version, info.connected
}

var fixedNow = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func callList(t *testing.T, h *OperatorPeers, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/exporter/operator_peers"+query, nil)
	rec := httptest.NewRecorder()
	api.Handler(h.List).ServeHTTP(rec, req)
	return rec
}

func TestOperatorPeers_JoinsEvidenceWithPeerInfo(t *testing.T) {
	pa, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	pb, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	source := &fakeEvidenceSource{evidence: []attribution.OperatorEvidence{{
		OperatorID: 185,
		Samples:    10,
		Candidates: []attribution.Candidate{
			{PeerID: pa, Count: 9, Share: 0.9},
			{PeerID: pb, Count: 1, Share: 0.1},
		},
	}}}
	peers := fakePeerInfo{pa: {version: "v2.5.0-41069c1f", connected: true}}
	h := newOperatorPeers(source, peers, func() time.Time { return fixedNow })

	rec := callList(t, h, "?operator_ids=185")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []spectypes.OperatorID{185}, source.gotOps)
	var resp operatorPeersResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, fixedNow.Equal(resp.GeneratedAt), "generated_at = %s", resp.GeneratedAt)
	require.Equal(t, int64(3600), resp.WindowSeconds)
	require.Equal(t, []operatorEvidenceJSON{{
		OperatorID: 185,
		Samples:    10,
		Candidates: []candidateJSON{
			{PeerID: pa.String(), Count: 9, Share: 0.9, Version: "v2.5.0-41069c1f", Connected: true},
			{PeerID: pb.String(), Count: 1, Share: 0.1, Version: "", Connected: false},
		},
	}}, resp.Operators)
}

func TestOperatorPeers_EmptyTracker(t *testing.T) {
	h := newOperatorPeers(&fakeEvidenceSource{}, fakePeerInfo{}, func() time.Time { return fixedNow })

	rec := callList(t, h, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"generated_at":"2026-10-15T09:00:00Z","window_seconds":3600,"operators":[]}`, rec.Body.String())
}

// TestOperatorPeers_JSONContract pins the exact response shape and tag names that the rollout
// tracker's Python consumer depends on.
func TestOperatorPeers_JSONContract(t *testing.T) {
	p, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	source := &fakeEvidenceSource{evidence: []attribution.OperatorEvidence{{
		OperatorID: 185,
		Samples:    412,
		Candidates: []attribution.Candidate{{PeerID: p, Count: 400, Share: 0.5}},
	}}}
	peers := fakePeerInfo{p: {version: "v2.5.0-41069c1f", connected: true}}
	subSecond := time.Date(2026, 10, 15, 9, 0, 0, 987654321, time.UTC)
	h := newOperatorPeers(source, peers, func() time.Time { return subSecond })

	rec := callList(t, h, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"generated_at": "2026-10-15T09:00:00Z",
		"window_seconds": 3600,
		"operators": [{
			"operator_id": 185,
			"samples": 412,
			"candidates": [{
				"peer_id": "`+p.String()+`",
				"count": 400,
				"share": 0.5,
				"version": "v2.5.0-41069c1f",
				"connected": true
			}]
		}]
	}`, rec.Body.String())
}

func TestOperatorPeers_BadOperatorIDs(t *testing.T) {
	h := newOperatorPeers(&fakeEvidenceSource{}, fakePeerInfo{}, func() time.Time { return fixedNow })

	rec := callList(t, h, "?operator_ids=7,abc")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `invalid operator id \"abc\"`)
}

func TestParseOperatorIDs(t *testing.T) {
	ids, err := parseOperatorIDs("")
	require.NoError(t, err)
	require.Empty(t, ids)

	ids, err = parseOperatorIDs(" 7, 9,, ")
	require.NoError(t, err)
	require.Equal(t, []spectypes.OperatorID{7, 9}, ids)

	_, err = parseOperatorIDs("-1")
	require.Error(t, err)
}

type fakeNodeInfoIndex map[peer.ID]*records.NodeInfo

func (f fakeNodeInfoIndex) NodeInfo(id peer.ID) *records.NodeInfo { return f[id] }

type fakeConnectedness map[peer.ID]network.Connectedness

func (f fakeConnectedness) Connectedness(id peer.ID) network.Connectedness { return f[id] }

func TestPeerInfoResolver(t *testing.T) {
	withMeta, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	noMeta, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	unknown, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	r := NewPeerInfoResolver(
		fakeNodeInfoIndex{
			withMeta: {Metadata: &records.NodeMetadata{NodeVersion: "v2.4.3-a1bc"}},
			noMeta:   {},
		},
		fakeConnectedness{withMeta: network.Connected, noMeta: network.Connected},
	)

	v, c := r.PeerInfo(withMeta)
	require.Equal(t, "v2.4.3-a1bc", v)
	require.True(t, c)

	v, c = r.PeerInfo(noMeta)
	require.Equal(t, "", v)
	require.True(t, c)

	v, c = r.PeerInfo(unknown)
	require.Equal(t, "", v)
	require.False(t, c)
}
