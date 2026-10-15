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
	source    string
	probedAt  time.Time
	connected bool
}

func (f fakePeerInfo) PeerInfo(id peer.ID) PeerInfo {
	info := f[id]
	return PeerInfo{Version: info.version, VersionSource: info.source, ProbedAt: info.probedAt, Connected: info.connected}
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
	peers := fakePeerInfo{pa: {version: "v2.5.0-41069c1f", source: "handshake", connected: true}}
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
			{PeerID: pa.String(), Count: 9, Share: 0.9, Version: "v2.5.0-41069c1f", VersionSource: "handshake", Connected: true},
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
	peers := fakePeerInfo{p: {version: "v2.5.0-41069c1f", source: "handshake", connected: true}}
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
				"version_source": "handshake",
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
		nil,
	)

	i := r.PeerInfo(withMeta)
	require.Equal(t, "v2.4.3-a1bc", i.Version)
	require.True(t, i.Connected)

	i = r.PeerInfo(noMeta)
	require.Equal(t, "", i.Version)
	require.True(t, i.Connected)

	i = r.PeerInfo(unknown)
	require.Equal(t, "", i.Version)
	require.False(t, i.Connected)
}

type fakeProbed map[peer.ID]struct {
	version string
	at      time.Time
}

func (f fakeProbed) Version(id peer.ID) (string, time.Time, bool) {
	e, ok := f[id]
	return e.version, e.at, ok
}

func TestPeerInfoPrecedence(t *testing.T) {
	id, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	probedAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	handshake := fakeNodeInfoIndex{id: {Metadata: &records.NodeMetadata{NodeVersion: "v2.5.0-hs"}}}
	probed := fakeProbed{id: {version: "v2.4.0-pr", at: probedAt}}
	conn := fakeConnectedness{id: network.Connected}

	tests := []struct {
		name       string
		index      fakeNodeInfoIndex
		probes     ProbedVersions
		wantVer    string
		wantSource string
		wantAt     time.Time
	}{
		{"handshake only", handshake, fakeProbed{}, "v2.5.0-hs", "handshake", time.Time{}},
		{"probe only", fakeNodeInfoIndex{}, probed, "v2.4.0-pr", "probe", probedAt},
		{"both: handshake wins", handshake, probed, "v2.5.0-hs", "handshake", time.Time{}},
		{"neither", fakeNodeInfoIndex{}, fakeProbed{}, "", "", time.Time{}},
		{"nil probes", fakeNodeInfoIndex{}, nil, "", "", time.Time{}},
		{"empty handshake version falls to probe", fakeNodeInfoIndex{id: {Metadata: &records.NodeMetadata{}}}, probed, "v2.4.0-pr", "probe", probedAt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewPeerInfoResolver(tc.index, conn, tc.probes)
			got := r.PeerInfo(id)
			require.Equal(t, tc.wantVer, got.Version)
			require.Equal(t, tc.wantSource, got.VersionSource)
			require.True(t, tc.wantAt.Equal(got.ProbedAt))
			require.True(t, got.Connected)
		})
	}
}

func TestOperatorPeers_ProbeVersionSourceJSON(t *testing.T) {
	pp, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	ph, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	source := &fakeEvidenceSource{evidence: []attribution.OperatorEvidence{{
		OperatorID: 7, Samples: 5,
		Candidates: []attribution.Candidate{{PeerID: pp, Count: 3, Share: 0.6}, {PeerID: ph, Count: 2, Share: 0.4}},
	}}}
	peers := fakePeerInfo{
		pp: {version: "v2.4.0-pr", source: "probe", probedAt: time.Date(2026, 10, 15, 12, 0, 0, 500, time.FixedZone("x", 3600))},
		ph: {version: "v2.5.0-hs", source: "handshake", connected: true},
	}
	rec := callList(t, newOperatorPeers(source, peers, func() time.Time { return fixedNow }), "")
	require.Equal(t, http.StatusOK, rec.Code)

	var raw struct {
		Operators []struct {
			Candidates []map[string]any `json:"candidates"`
		} `json:"operators"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	c := raw.Operators[0].Candidates
	require.Equal(t, "probe", c[0]["version_source"])
	require.Equal(t, "2026-10-15T11:00:00Z", c[0]["probed_at"])
	require.Equal(t, "handshake", c[1]["version_source"])
	require.NotContains(t, c[1], "probed_at")
	require.Contains(t, rec.Body.String(), `"version_source":"probe"`)
	require.Contains(t, rec.Body.String(), `"probed_at":"2026-10-15T11:00:00Z"`)
}
