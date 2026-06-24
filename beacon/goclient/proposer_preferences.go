package goclient

import (
	"context"
	"errors"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// SubmitProposerPreferences broadcasts signed Gloas (ePBS) proposer preferences (SIP #94 §5).
// beacon-APIs exposes no validator-facing publication endpoint yet (the BN shape is TBD), so this is
// a stub that errors rather than silently dropping them; swap in a real client once it lands.
func (*GoClient) SubmitProposerPreferences(_ context.Context, _ []*gloas.SignedProposerPreferences) error {
	return errors.New("submit proposer preferences: no beacon-API publication endpoint available upstream yet (SIP #94 §5)")
}
