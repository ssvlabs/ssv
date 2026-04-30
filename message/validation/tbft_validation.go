package validation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
)

// validateTBFTMessage decodes and sanity-checks a TBFT envelope carried
// in `signedSSVMessage.Data`. It does NOT verify the inner partial BLS
// signatures — that's handled by the runner's TBFT controller (via the
// share-bound Signer's VerifyPartial). This function only enforces:
//
//   - The envelope is well-formed (versioned wire.Envelope decode).
//   - Sender count == 1 (TBFT envelopes are operator-signed, single signer).
//   - The signer is a member of the validator's committee (already covered
//     by committeeChecks; included here as a defensive belt-and-braces).
//
// Layer / height bounds and per-cluster duplicate rate-limiting live in
// the runner (see protocol/v2/ssv/runner/tbft.RateLimiter): they need
// per-instance state which the message-validation layer does not own.
//
// Returns the parsed envelope on success so the caller can stash it on
// the queue.SSVMessage.Body for the dispatcher.
func (mv *messageValidator) validateTBFTMessage(
	_ context.Context,
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	_ peer.ID,
	_ time.Time,
) (*wire.Envelope, error) {
	if len(signedSSVMessage.OperatorIDs) != 1 {
		return nil, fmt.Errorf("TBFT envelope must have exactly one signer, got %d", len(signedSSVMessage.OperatorIDs))
	}

	signer := signedSSVMessage.OperatorIDs[0]
	if !committeeContains(committeeInfo.committee, signer) {
		return nil, fmt.Errorf("TBFT envelope signer %d not in committee", signer)
	}

	env, err := wire.Unwrap(signedSSVMessage.SSVMessage.Data)
	if err != nil {
		return nil, fmt.Errorf("decode TBFT envelope: %w", err)
	}
	if env == nil {
		return nil, errors.New("decoded TBFT envelope is nil")
	}
	return env, nil
}

func committeeContains(committee []spectypes.OperatorID, op spectypes.OperatorID) bool {
	for _, m := range committee {
		if m == op {
			return true
		}
	}
	return false
}
