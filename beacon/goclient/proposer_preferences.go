package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// proposerPreferencesPath is the SIP #94 §5 publish endpoint; go-eth2-client has no Gloas types, so
// SubmitProposerPreferences is a hand-rolled JSON POST.
const proposerPreferencesPath = "/eth/v1/validator/proposer_preferences"

// ProposerDutiesDependentRoot returns the dependent root from the v2 proposer-duties response for the
// given epoch — the proposer-lookahead seed a preference is pinned to (SIP #94 §5; callers pass the
// proposal slot's epoch). Gloas's v2 endpoint computes this root under the new proposer-lookahead
// seed; go-eth2-client drops the field, so this is a raw-HTTP fetch (the same interim surface as the
// PTC endpoints) until the fork exposes it.
func (gc *GoClient) ProposerDutiesDependentRoot(ctx context.Context, epoch phase0.Epoch) (phase0.Root, error) {
	// Several proposer-preferences runners (one per local proposing validator in the epoch) request the
	// same epoch's dependent_root concurrently; collapse that burst into a single GET. Not TTL-cached so
	// a reorg re-emission still observes a fresh root. The collapsed call runs on a detached context so
	// the leader caller's cancellation is not propagated to the concurrent waiters — they may have
	// later deadlines (different proposal slots), and a leader with a tight budget must not cancel a
	// waiter that still had room. An outer common-timeout still bounds the detached call.
	root, err, _ := gc.proposerDutiesDependentRootInflight.Do(epoch, func() (phase0.Root, error) {
		detached := context.WithoutCancel(ctx)
		dctx, cancel := context.WithTimeout(detached, gc.commonTimeout)
		defer cancel()
		return firstClientResult(dctx, gc, "ProposerDutiesDependentRoot", http.MethodGet, func(ctx context.Context, addr string) (phase0.Root, error) {
			// phase0.Root.UnmarshalJSON parses and length-checks the "0x…" hex, so decode straight into it.
			var resp struct {
				DependentRoot phase0.Root `json:"dependent_root"`
			}
			url := addr + fmt.Sprintf("/eth/v2/validator/duties/proposer/%d", epoch)
			if err := ptcDo(ctx, ptcHTTPClient, http.MethodGet, url, nil, nil, &resp); err != nil {
				return phase0.Root{}, err
			}
			return resp.DependentRoot, nil
		})
	})
	return root, err
}

// SubmitProposerPreferences broadcasts signed Gloas (ePBS) proposer preferences (SIP #94 §5) to every
// beacon client, succeeding if at least one accepts them; each BN verifies them and gossips on the
// proposer_preferences topic.
func (gc *GoClient) SubmitProposerPreferences(ctx context.Context, preferences []*gloas.SignedProposerPreferences) error {
	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitProposerPreferences", func(ctx context.Context, client Client) error {
		return submitProposerPreferences(ctx, ptcHTTPClient, gc.clientAddresses[client], preferences)
	})
}

// submitProposerPreferences POSTs the signed proposer preferences as a JSON array to the validator endpoint.
func submitProposerPreferences(ctx context.Context, httpClient *http.Client, addr string, preferences []*gloas.SignedProposerPreferences) error {
	body, err := json.Marshal(preferences)
	if err != nil {
		return fmt.Errorf("marshal proposer preferences: %w", err)
	}
	headers := map[string]string{"Eth-Consensus-Version": consensusVersionGloas}
	return ptcDo(ctx, httpClient, http.MethodPost, addr+proposerPreferencesPath, body, headers, nil)
}
