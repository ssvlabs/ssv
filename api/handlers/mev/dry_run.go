package mev

import (
	"net/http"
	"strconv"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

type DryRunProvider interface {
	Comparisons(limit int) []runner.MEVDryRunComparison
}

type Handler struct {
	dryRun DryRunProvider
}

func New(dryRun DryRunProvider) *Handler {
	return &Handler{dryRun: dryRun}
}

func (h *Handler) DryRunComparisons(w http.ResponseWriter, r *http.Request) error {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	comparisons := make([]runner.MEVDryRunComparison, 0)
	if h != nil && h.dryRun != nil {
		comparisons = h.dryRun.Comparisons(limit)
		if comparisons == nil {
			comparisons = make([]runner.MEVDryRunComparison, 0)
		}
	}

	resp := struct {
		Comparisons []runner.MEVDryRunComparison `json:"comparisons"`
	}{
		Comparisons: comparisons,
	}
	return api.Render(w, r, resp)
}
