package exporter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter/versionprobe"
)

type fakeProbeEntries []versionprobe.Entry

func (f fakeProbeEntries) Entries() []versionprobe.Entry { return f }

func TestVersionProbesList(t *testing.T) {
	p1, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	p2, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	at := time.Date(2026, 10, 15, 11, 59, 0, 999, time.UTC)
	entries := fakeProbeEntries{
		{
			PeerID: p1, Version: "v2.5.0-abc", ProbedAt: at, LastAttempt: at,
			Outcome: versionprobe.Outcome("ok"), Attempts: 2, NextAttempt: at.Add(time.Hour),
		},
		{
			PeerID: p2, LastAttempt: at.Add(-time.Minute), Outcome: versionprobe.Outcome("no_addr"),
			Error: "no address known", Attempts: 1, NextAttempt: at.Add(15 * time.Minute),
		},
	}
	h := &VersionProbes{source: entries, now: func() time.Time { return fixedNow }}

	rec := httptest.NewRecorder()
	api.Handler(h.List).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/exporter/version_probes", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	require.JSONEq(t, `{
		"generated_at": "2026-10-15T09:00:00Z",
		"probes": [
			{"peer_id": "`+p1.String()+`", "version": "v2.5.0-abc", "probed_at": "2026-10-15T11:59:00Z",
			 "last_attempt": "2026-10-15T11:59:00Z", "outcome": "ok", "attempts": 2, "next_attempt": "2026-10-15T12:59:00Z"},
			{"peer_id": "`+p2.String()+`", "version": "", "last_attempt": "2026-10-15T11:58:00Z",
			 "outcome": "no_addr", "error": "no address known", "attempts": 1, "next_attempt": "2026-10-15T12:14:00Z"}
		]
	}`, rec.Body.String())

	var raw struct {
		Probes []map[string]json.RawMessage `json:"probes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.NotContains(t, raw.Probes[1], "probed_at")
	require.NotContains(t, raw.Probes[0], "error")
}

func TestVersionProbesList_Empty(t *testing.T) {
	h := NewVersionProbes(fakeProbeEntries(nil))
	rec := httptest.NewRecorder()
	api.Handler(h.List).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"probes":[]`)
}
