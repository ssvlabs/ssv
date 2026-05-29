package validation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	twoabwire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// validateTwoabMessage is the 2abOBFT sibling of validateOBFTMessage: it
// decodes and sanity-checks a 2abOBFT envelope carried in
// `signedSSVMessage.Data`, then verifies its inner BLS / IBE-tag material
// against the validator's pub-key shares at the validation boundary, before it
// reaches the consensus-critical path.
//
// It reuses the variant-neutral pieces of the OBFT validator: the slot-window
// bounds (obftAllowed{Past,Future}Slots), the admission tracker (keyed by an
// opaque `kind byte`, so the 2ab kinds — including the Phase-2a Value/NoValue
// envelopes — bucket correctly), and committeeContains. Only the wire codec,
// the per-kind slot/signer extraction, and the verifier method set differ.
//
// What this verifies vs defers mirrors validateOBFTMessage, adapted to the 2ab
// shapes: σ on ValueMsg.L0Partial and Phase1Bundle.LeaderSigma; NR partials on
// Commit.L0Partial + NRPlaintext LayerEntries; the certificate aggregate. The
// forwarded ValueMsg witnesses and chained-IBE σ entries are left to the
// protocol layer (see protocol/v2/obft/twoab/verify.go).
func (mv *messageValidator) validateTwoabMessage(
	_ context.Context,
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	_ peer.ID,
	_ time.Time,
) (*twoabwire.Envelope, error) {
	if len(signedSSVMessage.OperatorIDs) != 1 {
		return nil, fmt.Errorf("2abOBFT envelope must have exactly one signer, got %d", len(signedSSVMessage.OperatorIDs))
	}

	signer := signedSSVMessage.OperatorIDs[0]
	if !committeeContains(committeeInfo.committee, signer) {
		return nil, fmt.Errorf("2abOBFT envelope signer %d not in committee", signer)
	}

	env, err := twoabwire.Unwrap(signedSSVMessage.SSVMessage.Data)
	if err != nil {
		return nil, fmt.Errorf("decode 2abOBFT envelope: %w", err)
	}
	if env == nil {
		return nil, errors.New("decoded 2abOBFT envelope is nil")
	}

	// Slot-window check (same bounds as OBFT — both complete in-slot).
	envSlot, err := twoabEnvelopeSlot(env)
	if err != nil {
		return nil, fmt.Errorf("2abOBFT envelope: extract slot: %w", err)
	}
	current := mv.netCfg.EstimatedCurrentSlot()
	if envSlot+obftAllowedPastSlots < current {
		return nil, fmt.Errorf("2abOBFT envelope: slot %d too old (current %d)", envSlot, current)
	}
	if envSlot > current+obftAllowedFutureSlots {
		return nil, fmt.Errorf("2abOBFT envelope: slot %d too far in future (current %d)", envSlot, current)
	}

	// Admission cap pre-BLS. The tracker is keyed by an opaque kind byte, so it
	// serves both variants (an operator runs exactly one); reusing it bounds
	// the distinct-content per (msgID, slot, op, kind) bucket before paying the
	// BLS verify cost.
	if mv.consensusAdmissions != nil {
		if err := mv.consensusAdmissions.Admit(
			signedSSVMessage.SSVMessage.GetID(),
			envSlot,
			signer,
			byte(env.Kind),
			signedSSVMessage.SSVMessage.Data,
		); err != nil {
			return nil, err
		}
	}

	share, ok := mv.validatorStore.Validator(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID())
	if !ok {
		return nil, fmt.Errorf("2abOBFT envelope: unknown validator %x",
			signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID())
	}
	// Cached per-validator Verifier — same rationale + invalidation as the
	// OBFT path (see verifier_cache.go).
	verifier, err := mv.twoabVerifierFor(share)
	if err != nil {
		return nil, fmt.Errorf("construct 2abOBFT verifier: %w", err)
	}

	// Defense-in-depth: inner claimed signer must match the outer signer.
	if err := verifyTwoabInnerSignerMatchesOuter(env, signer); err != nil {
		return nil, err
	}

	if err := verifyTwoabEnvelopeCrypto(env, verifier); err != nil {
		return nil, err
	}

	return env, nil
}

// verifyTwoabInnerSignerMatchesOuter ensures the 2abOBFT envelope's claimed
// inner signer matches the outer SignedSSVMessage signer. Phase1Bundle / Value
// / NoValue / Commit carry an inner OperatorID; Certificate does not (the outer
// signer is its only identity binding).
func verifyTwoabInnerSignerMatchesOuter(env *twoabwire.Envelope, outerSigner spectypes.OperatorID) error {
	mismatch := func(inner twoabcore.OperatorID) error {
		return fmt.Errorf("2abOBFT envelope: outer signer %d != inner OperatorID %d", outerSigner, inner)
	}
	switch env.Kind {
	case twoabwire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return errors.New("2abOBFT envelope: nil Phase1Bundle body")
		}
		if twoabcore.OperatorID(outerSigner) != env.Phase1Bundle.OperatorID {
			return mismatch(env.Phase1Bundle.OperatorID)
		}
	case twoabwire.KindValue:
		if env.ValueMsg == nil {
			return errors.New("2abOBFT envelope: nil ValueMsg body")
		}
		if twoabcore.OperatorID(outerSigner) != env.ValueMsg.OperatorID {
			return mismatch(env.ValueMsg.OperatorID)
		}
	case twoabwire.KindNoValue:
		if env.NoValueMsg == nil {
			return errors.New("2abOBFT envelope: nil NoValueMsg body")
		}
		if twoabcore.OperatorID(outerSigner) != env.NoValueMsg.OperatorID {
			return mismatch(env.NoValueMsg.OperatorID)
		}
	case twoabwire.KindCommit:
		if env.Commit == nil {
			return errors.New("2abOBFT envelope: nil Commit body")
		}
		if twoabcore.OperatorID(outerSigner) != env.Commit.OperatorID {
			return mismatch(env.Commit.OperatorID)
		}
	case twoabwire.KindCertificate:
		// No inner OperatorID; outer signer is the only identity binding.
	default:
		return fmt.Errorf("2abOBFT envelope: unknown kind 0x%02x", byte(env.Kind))
	}
	return nil
}

// verifyTwoabEnvelopeCrypto runs validator-time BLS / IBE-tag verification on
// the 2abOBFT envelope's inner cryptographic material.
func verifyTwoabEnvelopeCrypto(env *twoabwire.Envelope, verifier *twoabcore.Verifier) error {
	switch env.Kind {
	case twoabwire.KindPhase1Bundle:
		if err := verifier.VerifyPhase1Bundle(env.Phase1Bundle); err != nil {
			return fmt.Errorf("2abOBFT phase-1 bundle verification: %w", err)
		}
	case twoabwire.KindValue:
		if err := verifier.VerifyValueMsg(env.ValueMsg); err != nil {
			return fmt.Errorf("2abOBFT value-msg verification: %w", err)
		}
	case twoabwire.KindNoValue:
		if err := verifier.VerifyNoValueMsg(env.NoValueMsg); err != nil {
			return fmt.Errorf("2abOBFT no-value-msg verification: %w", err)
		}
	case twoabwire.KindCommit:
		if err := verifier.VerifyCommit(env.Commit); err != nil {
			return fmt.Errorf("2abOBFT commit verification: %w", err)
		}
	case twoabwire.KindCertificate:
		if err := verifier.VerifyCertificate(env.Certificate); err != nil {
			return fmt.Errorf("2abOBFT certificate verification: %w", err)
		}
	default:
		return fmt.Errorf("2abOBFT crypto verification: unknown envelope kind 0x%02x", byte(env.Kind))
	}
	return nil
}

// twoabEnvelopeSlot returns the slot (Height) the 2abOBFT envelope targets.
// Each kind carries it in a different body field.
func twoabEnvelopeSlot(env *twoabwire.Envelope) (phase0.Slot, error) {
	switch env.Kind {
	case twoabwire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return 0, errors.New("nil Phase1Bundle body")
		}
		return phase0.Slot(env.Phase1Bundle.Height), nil
	case twoabwire.KindValue:
		if env.ValueMsg == nil {
			return 0, errors.New("nil ValueMsg body")
		}
		return phase0.Slot(env.ValueMsg.Height), nil
	case twoabwire.KindNoValue:
		if env.NoValueMsg == nil {
			return 0, errors.New("nil NoValueMsg body")
		}
		return phase0.Slot(env.NoValueMsg.Height), nil
	case twoabwire.KindCommit:
		if env.Commit == nil {
			return 0, errors.New("nil Commit body")
		}
		return phase0.Slot(env.Commit.Height), nil
	case twoabwire.KindCertificate:
		if env.Certificate == nil {
			return 0, errors.New("nil Certificate body")
		}
		return phase0.Slot(env.Certificate.Height), nil
	default:
		return 0, fmt.Errorf("unknown envelope kind 0x%02x", byte(env.Kind))
	}
}
