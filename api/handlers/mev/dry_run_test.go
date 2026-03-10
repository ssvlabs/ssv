package mev

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

type stubDryRunProvider struct {
	lastLimit int
	out       []runner.MEVDryRunComparison
}

func (s *stubDryRunProvider) Comparisons(limit int) []runner.MEVDryRunComparison {
	s.lastLimit = limit
	return s.out
}

func TestDryRunComparisons_DefaultLimitAndShape(t *testing.T) {
	t.Parallel()

	p := &stubDryRunProvider{
		out: []runner.MEVDryRunComparison{
			{Slot: phase0.Slot(1)},
		},
	}
	h := New(p)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/mev/dry-run/comparisons", nil)
	err := h.DryRunComparisons(rr, req)
	require.NoError(t, err)
	require.Equal(t, 200, rr.Code)
	require.Equal(t, 100, p.lastLimit)

	var resp struct {
		Comparisons []runner.MEVDryRunComparison `json:"comparisons"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Comparisons, 1)
	require.Equal(t, phase0.Slot(1), resp.Comparisons[0].Slot)
}

func TestDryRunComparisons_RespectsLimitParam(t *testing.T) {
	t.Parallel()

	p := &stubDryRunProvider{}
	h := New(p)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/mev/dry-run/comparisons?limit=5", nil)
	err := h.DryRunComparisons(rr, req)
	require.NoError(t, err)
	require.Equal(t, 200, rr.Code)
	require.Equal(t, 5, p.lastLimit)
}

func TestDryRunComparisons_NilProviderReturnsEmptyArray(t *testing.T) {
	t.Parallel()

	h := New(nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/mev/dry-run/comparisons", nil)
	err := h.DryRunComparisons(rr, req)
	require.NoError(t, err)
	require.Equal(t, 200, rr.Code)

	var resp struct {
		Comparisons []runner.MEVDryRunComparison `json:"comparisons"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.NotNil(t, resp.Comparisons)
	require.Len(t, resp.Comparisons, 0)
}
