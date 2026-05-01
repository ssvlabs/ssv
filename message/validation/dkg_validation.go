package validation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// validateDKGMessage applies the minimum sanity checks to a DKG-ceremony
// envelope carried in `signedSSVMessage.Data`. Per docs/TBFT-DKG-TASKS.md
// (Phase E), envelope decoding is deferred to the per-cluster
// orchestrator (which owns the kyber suite); this function only enforces:
//
//   - The envelope is non-empty.
//   - Sender count == 1 (DKG envelopes are operator-signed, single signer).
//   - The signer is a member of the committee identified by the MsgID
//     (already covered by committeeChecks via the RoleDKG → committee
//     lookup; included here as a defensive belt-and-braces).
//
// Returns the raw envelope bytes on success so the caller can stash them
// on the queue.SSVMessage.Body for the dispatcher; the orchestrator
// later decodes via dkgwire.Unwrap.
func (mv *messageValidator) validateDKGMessage(
	_ context.Context,
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	_ peer.ID,
	_ time.Time,
) ([]byte, error) {
	if len(signedSSVMessage.OperatorIDs) != 1 {
		return nil, fmt.Errorf("DKG envelope must have exactly one signer, got %d", len(signedSSVMessage.OperatorIDs))
	}

	signer := signedSSVMessage.OperatorIDs[0]
	if !committeeContains(committeeInfo.committee, signer) {
		return nil, fmt.Errorf("DKG envelope signer %d not in committee", signer)
	}

	if len(signedSSVMessage.SSVMessage.Data) == 0 {
		return nil, errors.New("DKG envelope has empty body")
	}
	return signedSSVMessage.SSVMessage.Data, nil
}
