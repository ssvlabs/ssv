package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// builderPreferencesPath is the beacon-APIs#630 endpoint through which the beacon node forwards a
// proposer's ahead-of-time per-builder preferences (BN -> builder submitBuilderPreferences); go-eth2-client
// has no Gloas types, so SubmitBuilderPreferences is a hand-rolled JSON POST.
const builderPreferencesPath = "/eth/v1/validator/builder_preferences"

// SubmitBuilderPreferences submits the ahead-of-time per-builder preferences (issue #2962 phase 3) to
// every beacon client, succeeding if at least one accepts them; each beacon node forwards every entry to
// its builder's submitBuilderPreferences. The call is synchronous (bounded by commonTimeout) but
// best-effort: callers do not gate on the outcome — a failure only surfaces in metrics and logs.
func (gc *GoClient) SubmitBuilderPreferences(ctx context.Context, preferences []*gloas.BuilderPreferencesEntry) error {
	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitBuilderPreferences", func(ctx context.Context, client Client) error {
		return submitBuilderPreferences(ctx, ptcHTTPClient, gc.clientAddresses[client], preferences)
	})
}

// submitBuilderPreferences POSTs the preferences as a JSON array to the validator endpoint. A 404 is
// flagged as a missing route — a beacon node predating the merged beacon-APIs#630 endpoint — rather than
// a transient failure.
func submitBuilderPreferences(ctx context.Context, httpClient *http.Client, addr string, preferences []*gloas.BuilderPreferencesEntry) error {
	body, err := json.Marshal(preferences)
	if err != nil {
		return fmt.Errorf("marshal builder preferences: %w", err)
	}
	headers := map[string]string{"Eth-Consensus-Version": consensusVersionGloas}
	err = ptcDo(ctx, httpClient, http.MethodPost, addr+builderPreferencesPath, body, headers, nil)
	if isNotFound(err) {
		return fmt.Errorf("beacon node lacks the gloas builder_preferences endpoint (beacon-APIs#630): %w", err)
	}
	return err
}
