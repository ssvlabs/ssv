package goclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// ProposerDutiesDependentRoot returns the dependent root from the v2 proposer-duties response for the
// given epoch — the proposer-lookahead seed a preference is pinned to (SIP #94 §5; callers pass the
// proposal slot's epoch). Gloas's v2 endpoint computes this root under the new proposer-lookahead
// seed; go-eth2-client drops the field, so this is a raw-HTTP fetch (the same interim surface as the
// PTC endpoints) until the fork exposes it.
func (gc *GoClient) ProposerDutiesDependentRoot(ctx context.Context, epoch phase0.Epoch) (phase0.Root, error) {
	// Several proposer-preferences runners (one per local proposing validator in the epoch) request the
	// same epoch's dependent_root concurrently; collapse that burst into a single GET. Not TTL-cached so
	// a reorg re-emission still observes a fresh root. The collapsed call adopts the winning caller's
	// ctx, so its cancellation also fails the concurrent waiters — acceptable as they share the slot's
	// deadline window and a re-emit recovers.
	root, err, _ := gc.proposerDutiesDependentRootInflight.Do(epoch, func() (phase0.Root, error) {
		return firstClientResult(ctx, gc, "ProposerDutiesDependentRoot", http.MethodGet, func(ctx context.Context, addr string) (phase0.Root, error) {
			var resp struct {
				DependentRoot string `json:"dependent_root"`
			}
			url := addr + fmt.Sprintf("/eth/v2/validator/duties/proposer/%d", epoch)
			if err := ptcDo(ctx, ptcHTTPClient, http.MethodGet, url, nil, nil, &resp); err != nil {
				return phase0.Root{}, err
			}
			raw, err := hex.DecodeString(strings.TrimPrefix(resp.DependentRoot, "0x"))
			if err != nil {
				return phase0.Root{}, fmt.Errorf("decode dependent_root %q: %w", resp.DependentRoot, err)
			}
			var root phase0.Root
			if len(raw) != len(root) {
				return phase0.Root{}, fmt.Errorf("dependent_root: expected %d bytes, got %d", len(root), len(raw))
			}
			copy(root[:], raw)
			return root, nil
		})
	})
	return root, err
}

// SubmitProposerPreferences broadcasts signed Gloas (ePBS) proposer preferences (SIP #94 §5).
// beacon-APIs exposes no validator-facing publication endpoint yet (the BN shape is TBD), so this
// returns the gloas.ErrProposerPreferencesPublishUnavailable sentinel — which the runner treats as a
// benign no-op — rather than silently dropping them; swap in a real client once the endpoint lands.
func (*GoClient) SubmitProposerPreferences(_ context.Context, _ []*gloas.SignedProposerPreferences) error {
	return gloas.ErrProposerPreferencesPublishUnavailable
}
