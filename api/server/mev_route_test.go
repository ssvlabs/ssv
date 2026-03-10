package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	exporterHandlers "github.com/ssvlabs/ssv/api/handlers/exporter"
	mevHandlers "github.com/ssvlabs/ssv/api/handlers/mev"
	"github.com/ssvlabs/ssv/api/handlers/node"
	"github.com/ssvlabs/ssv/api/handlers/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

type stubDryRunProvider struct {
	out []runner.MEVDryRunComparison
}

func (s *stubDryRunProvider) Comparisons(limit int) []runner.MEVDryRunComparison {
	if limit <= 0 || limit >= len(s.out) {
		return s.out
	}
	return s.out[:limit]
}

func TestMEVDryRunRoute_MountedOnlyWhenHandlerProvided(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	n := &node.Node{}
	v := &validators.Validators{}
	e := &exporterHandlers.Exporter{}

	// Without MEV handler => 404.
	srvNoMEV := New(logger, ":0", n, v, e, nil, false)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/mev/dry-run/comparisons", nil)
	srvNoMEV.httpServer.Handler.ServeHTTP(rr, req)
	require.Equal(t, 404, rr.Code)

	// With MEV handler => 200 + JSON shape.
	provider := &stubDryRunProvider{
		out: []runner.MEVDryRunComparison{{Slot: phase0.Slot(1)}},
	}
	mev := mevHandlers.New(provider)
	srvMEV := New(logger, ":0", n, v, e, mev, false)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/mev/dry-run/comparisons?limit=1", nil)
	srvMEV.httpServer.Handler.ServeHTTP(rr, req)
	require.Equal(t, 200, rr.Code)

	var decoded struct {
		Comparisons []runner.MEVDryRunComparison `json:"comparisons"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&decoded))
	require.Len(t, decoded.Comparisons, 1)
	require.Equal(t, phase0.Slot(1), decoded.Comparisons[0].Slot)
}
