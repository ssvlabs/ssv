package validation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
)

// allowedFutureSlots caps how far in the future an OBFT envelope's claimed
// Height (slot) can be relative to the current estimated slot. Combined with
// LateSlotAllowance (the past-window), this bounds the (slot, op) state
// the runner's pre-instance buffer can accumulate from network input.
//
// Without this bound, a byzantine streaming envelopes at slot=0..2^64-1
// could fill the Controller's pending-envelope buffer (capped at
// MaxPendingSlots=256) entirely with attacker-chosen slot numbers, evicting
// legitimate near-term entries.
const allowedFutureSlots phase0.Slot = 4

// validateOBFTMessage decodes and sanity-checks an OBFT envelope carried
// in `signedSSVMessage.Data`. After decoding, it verifies the envelope's
// inner BLS / IBE-tag material against the validator's pub-key shares —
// rejecting messages with garbage cryptographic material at the validation
// boundary, before they reach the consensus-critical path.
//
// What this enforces:
//
//   - The envelope is well-formed (versioned wire.Envelope decode).
//   - Sender count == 1 (OBFT envelopes are operator-signed, single signer).
//   - The signer is a member of the validator's committee.
//   - For Phase1Bundle: σ_V verifies against the claimed leader's V-share.
//   - For Commit: each NR partial verifies against the claimed signer's
//     IBE pub-share (or V-share under Option A); each witness's σ_V verifies
//     against its claimed leader's V-share.
//   - For Certificate: the aggregate sig verifies against the cluster
//     V-pubkey.
//
// What this does NOT enforce (deferred to protocol layer to preserve
// evidence semantics):
//
//   - Commit L_0 σ entries: Rule 5 evidence depends on retained-V matching,
//     which requires per-instance state.
//   - Rule 2 evidence on distinct witness V's at same (layer, leader):
//     requires per-instance retention bookkeeping. (We verify witness σ
//     correctness here; equivocation detection still happens at protocol.)
//   - Commit deeper-layer σ entries: IBE-encrypted; verification requires
//     the chained-decryption key derived from NR-quorum during Phase 3.
//
// Returns the parsed envelope on success so the caller can stash it on
// the queue.SSVMessage.Body for the dispatcher.
func (mv *messageValidator) validateOBFTMessage(
	_ context.Context,
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	_ peer.ID,
	_ time.Time,
) (*wire.Envelope, error) {
	if len(signedSSVMessage.OperatorIDs) != 1 {
		return nil, fmt.Errorf("OBFT envelope must have exactly one signer, got %d", len(signedSSVMessage.OperatorIDs))
	}

	signer := signedSSVMessage.OperatorIDs[0]
	if !committeeContains(committeeInfo.committee, signer) {
		return nil, fmt.Errorf("OBFT envelope signer %d not in committee", signer)
	}

	env, err := wire.Unwrap(signedSSVMessage.SSVMessage.Data)
	if err != nil {
		return nil, fmt.Errorf("decode OBFT envelope: %w", err)
	}
	if env == nil {
		return nil, errors.New("decoded OBFT envelope is nil")
	}

	// Slot-window check. Reject envelopes whose claimed Height is outside
	// [currentSlot - LateSlotAllowance - SlotsPerEpoch, currentSlot +
	// allowedFutureSlots]. Without this, a byzantine could stream envelopes
	// for arbitrary future slot numbers and fill the runner's pre-instance
	// pending buffer with attacker-chosen entries.
	envSlot, err := envelopeSlot(env)
	if err != nil {
		return nil, fmt.Errorf("OBFT envelope: extract slot: %w", err)
	}
	current := mv.netCfg.EstimatedCurrentSlot()
	maxPast := phase0.Slot(mv.netCfg.SlotsPerEpoch + LateSlotAllowance)
	if envSlot+maxPast < current {
		return nil, fmt.Errorf("OBFT envelope: slot %d too old (current %d)", envSlot, current)
	}
	if envSlot > current+allowedFutureSlots {
		return nil, fmt.Errorf("OBFT envelope: slot %d too far in future (current %d)", envSlot, current)
	}

	// Look up the validator's share to construct a verifier. OBFT messages
	// are per-validator (proposer role); the executor ID is the validator
	// pubkey.
	share, ok := mv.validatorStore.Validator(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID())
	if !ok {
		return nil, fmt.Errorf("OBFT envelope: unknown validator %x",
			signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID())
	}
	verifier, err := obftadapter.NewVerifierFromShare(&share.Share, nil /* IBE shares: Option A fallback */)
	if err != nil {
		return nil, fmt.Errorf("construct OBFT verifier: %w", err)
	}

	// Defense-in-depth: verify the inner-message claimed signer matches the
	// outer-envelope signer. Without this, a byzantine could sign the outer
	// envelope themselves but claim a different inner OperatorID; the
	// per-share BLS check would then verify against the WRONG share.
	if err := verifyInnerSignerMatchesOuter(env, signer); err != nil {
		return nil, err
	}

	if err := verifyEnvelopeCrypto(env, verifier); err != nil {
		return nil, err
	}

	return env, nil
}

// verifyInnerSignerMatchesOuter ensures the OBFT envelope's claimed inner
// signer (Phase1Bundle.OperatorID, Commit.OperatorID — Certificate has no
// signer field) matches the outer SignedSSVMessage's signer.
func verifyInnerSignerMatchesOuter(env *wire.Envelope, outerSigner spectypes.OperatorID) error {
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return errors.New("OBFT envelope: nil Phase1Bundle body")
		}
		if obftcore.OperatorID(outerSigner) != env.Phase1Bundle.OperatorID {
			return fmt.Errorf("OBFT envelope: outer signer %d != inner OperatorID %d",
				outerSigner, env.Phase1Bundle.OperatorID)
		}
	case wire.KindCommit:
		if env.Commit == nil {
			return errors.New("OBFT envelope: nil Commit body")
		}
		if obftcore.OperatorID(outerSigner) != env.Commit.OperatorID {
			return fmt.Errorf("OBFT envelope: outer signer %d != inner OperatorID %d",
				outerSigner, env.Commit.OperatorID)
		}
	case wire.KindCertificate:
		// Certificate has no inner OperatorID; outer signer is the only
		// identity binding, no extra check needed.
	}
	return nil
}

// verifyEnvelopeCrypto runs validator-time BLS / IBE-tag verification on the
// envelope's inner cryptographic material.
func verifyEnvelopeCrypto(env *wire.Envelope, verifier *obftcore.Verifier) error {
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if err := verifier.VerifyPhase1Bundle(env.Phase1Bundle); err != nil {
			return fmt.Errorf("OBFT phase-1 bundle verification: %w", err)
		}
	case wire.KindCommit:
		if err := verifier.VerifyCommitNRPartials(env.Commit); err != nil {
			return fmt.Errorf("OBFT commit NR-partial verification: %w", err)
		}
		if err := verifier.VerifyCommitWitnesses(env.Commit); err != nil {
			return fmt.Errorf("OBFT commit witness verification: %w", err)
		}
	case wire.KindCertificate:
		if err := verifier.VerifyCertificate(env.Certificate); err != nil {
			return fmt.Errorf("OBFT certificate verification: %w", err)
		}
	}
	return nil
}

// envelopeSlot returns the slot (Height) the envelope is targeting.
// Each envelope kind carries it in a different field; centralized here
// so the validation layer can reason about it uniformly.
func envelopeSlot(env *wire.Envelope) (phase0.Slot, error) {
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return 0, errors.New("nil Phase1Bundle body")
		}
		return phase0.Slot(env.Phase1Bundle.Height), nil
	case wire.KindCommit:
		if env.Commit == nil {
			return 0, errors.New("nil Commit body")
		}
		return phase0.Slot(env.Commit.Height), nil
	case wire.KindCertificate:
		if env.Certificate == nil {
			return 0, errors.New("nil Certificate body")
		}
		return phase0.Slot(env.Certificate.Height), nil
	default:
		return 0, fmt.Errorf("unknown envelope kind 0x%02x", byte(env.Kind))
	}
}

func committeeContains(committee []spectypes.OperatorID, op spectypes.OperatorID) bool {
	for _, m := range committee {
		if m == op {
			return true
		}
	}
	return false
}
