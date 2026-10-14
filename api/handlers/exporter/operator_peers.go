package exporter

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter/attribution"
)

// OperatorEvidenceSource provides per-operator first-delivery evidence.
type OperatorEvidenceSource interface {
	Snapshot(ops []spectypes.OperatorID) []attribution.OperatorEvidence
	WindowDuration() time.Duration
}

// PeerInfoResolver describes a peer as currently known to this node.
type PeerInfoResolver interface {
	PeerInfo(id peer.ID) (version string, connected bool)
}

// OperatorPeers serves GET /v1/exporter/operator_peers: for each operator, the peers that most
// often first-delivered its signed messages, with each peer's handshake version.
type OperatorPeers struct {
	source OperatorEvidenceSource
	peers  PeerInfoResolver
	now    func() time.Time
}

// NewOperatorPeers returns the handler backed by an attribution source and the peers index.
func NewOperatorPeers(source OperatorEvidenceSource, peers PeerInfoResolver) *OperatorPeers {
	return newOperatorPeers(source, peers, time.Now)
}

func newOperatorPeers(source OperatorEvidenceSource, peers PeerInfoResolver, now func() time.Time) *OperatorPeers {
	return &OperatorPeers{source: source, peers: peers, now: now}
}

type operatorPeersResponse struct {
	GeneratedAt   time.Time              `json:"generated_at"`
	WindowSeconds int64                  `json:"window_seconds"`
	Operators     []operatorEvidenceJSON `json:"operators"`
}

type operatorEvidenceJSON struct {
	OperatorID uint64          `json:"operator_id"`
	Samples    uint64          `json:"samples"`
	Candidates []candidateJSON `json:"candidates"`
}

type candidateJSON struct {
	PeerID    string  `json:"peer_id"`
	Count     uint64  `json:"count"`
	Share     float64 `json:"share"`
	Version   string  `json:"version"`
	Connected bool    `json:"connected"`
}

// List handles GET /v1/exporter/operator_peers[?operator_ids=1,2,3].
func (h *OperatorPeers) List(w http.ResponseWriter, r *http.Request) error {
	ops, err := parseOperatorIDs(r.URL.Query().Get("operator_ids"))
	if err != nil {
		return api.BadRequestError(err)
	}

	evidence := h.source.Snapshot(ops)
	resp := operatorPeersResponse{
		GeneratedAt:   h.now().UTC().Truncate(time.Second),
		WindowSeconds: int64(h.source.WindowDuration() / time.Second),
		Operators:     make([]operatorEvidenceJSON, 0, len(evidence)),
	}
	for _, ev := range evidence {
		item := operatorEvidenceJSON{
			OperatorID: ev.OperatorID,
			Samples:    ev.Samples,
			Candidates: make([]candidateJSON, 0, len(ev.Candidates)),
		}
		for _, c := range ev.Candidates {
			version, connected := h.peers.PeerInfo(c.PeerID)
			item.Candidates = append(item.Candidates, candidateJSON{
				PeerID:    c.PeerID.String(),
				Count:     c.Count,
				Share:     c.Share,
				Version:   version,
				Connected: connected,
			})
		}
		resp.Operators = append(resp.Operators, item)
	}
	return api.Render(w, r, resp)
}

// parseOperatorIDs parses a comma-separated list of operator IDs. Blank entries are ignored.
func parseOperatorIDs(raw string) ([]spectypes.OperatorID, error) {
	var ids []spectypes.OperatorID
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid operator id %q", part)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
