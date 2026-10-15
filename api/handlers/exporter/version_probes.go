package exporter

import (
	"net/http"
	"time"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter/versionprobe"
)

// ProbeEntries lists the peer version probe results, newest attempt first.
type ProbeEntries interface {
	Entries() []versionprobe.Entry
}

// VersionProbes serves GET /v1/exporter/version_probes: the raw probe results, for diagnostics.
type VersionProbes struct {
	source ProbeEntries
	now    func() time.Time
}

// NewVersionProbes returns the handler backed by the probe cache.
func NewVersionProbes(source ProbeEntries) *VersionProbes {
	return &VersionProbes{source: source, now: time.Now}
}

type versionProbesResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Probes      []versionProbeJSON `json:"probes"`
}

type versionProbeJSON struct {
	PeerID      string     `json:"peer_id"`
	Version     string     `json:"version"`
	ProbedAt    *time.Time `json:"probed_at,omitempty"`
	LastAttempt time.Time  `json:"last_attempt"`
	Outcome     string     `json:"outcome"`
	Error       string     `json:"error,omitempty"`
	Attempts    int        `json:"attempts"`
	NextAttempt time.Time  `json:"next_attempt"`
}

// List handles GET /v1/exporter/version_probes.
func (h *VersionProbes) List(w http.ResponseWriter, r *http.Request) error {
	entries := h.source.Entries()
	resp := versionProbesResponse{
		GeneratedAt: h.now().UTC().Truncate(time.Second),
		Probes:      make([]versionProbeJSON, 0, len(entries)),
	}
	for _, e := range entries {
		resp.Probes = append(resp.Probes, versionProbeJSON{
			PeerID:      e.PeerID.String(),
			Version:     e.Version,
			ProbedAt:    utcSecondPtr(e.ProbedAt),
			LastAttempt: e.LastAttempt.UTC().Truncate(time.Second),
			Outcome:     string(e.Outcome),
			Error:       e.Error,
			Attempts:    e.Attempts,
			NextAttempt: e.NextAttempt.UTC().Truncate(time.Second),
		})
	}
	return api.Render(w, r, resp)
}
