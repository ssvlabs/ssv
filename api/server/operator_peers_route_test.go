package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	exporterHandlers "github.com/ssvlabs/ssv/api/handlers/exporter"
	"github.com/ssvlabs/ssv/exporter/attribution"
)

type emptySource struct{}

func (emptySource) Snapshot([]spectypes.OperatorID) []attribution.OperatorEvidence { return nil }
func (emptySource) WindowDuration() time.Duration                                  { return time.Hour }

type noPeers struct{}

func (noPeers) PeerInfo(peer.ID) (string, bool) { return "", false }

func get(s *Server, path string) int {
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func TestServer_OperatorPeersRouteOnlyWhenEnabled(t *testing.T) {
	disabled := New(zap.NewNop(), "", nil, nil, nil, nil, false)
	require.Equal(t, http.StatusNotFound, get(disabled, "/v1/exporter/operator_peers"))

	enabled := New(zap.NewNop(), "", nil, nil, nil, exporterHandlers.NewOperatorPeers(emptySource{}, noPeers{}), false)
	require.Equal(t, http.StatusOK, get(enabled, "/v1/exporter/operator_peers"))
}
