package runner

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// reconstructQuorumSig reconstructs share's validator signature over root from container's quorum. The
// reconstruction combines every share of the root, so on failure each share is verified and the bad ones
// dropped. If that leaves the root below quorum, the failure is recoverable (recoverableReconstructError): a
// later share brings it back. If the root is still at quorum, the reconstruction is retried on the remaining
// shares, as no later share would cross its quorum again. With no bad share to drop, more shares can't fix
// it, and the failure is terminal.
func (b *BaseRunner) reconstructQuorumSig(container *ssv.PartialSigContainer, root [32]byte, share *spectypes.Share, phase string) (phase0.BLSSignature, error) {
	for {
		sig, err := b.State.ReconstructBeaconSig(container, root, share.ValidatorPubKey[:], share.ValidatorIndex)
		if err == nil {
			var specSig phase0.BLSSignature
			copy(specSig[:], sig)
			return specSig, nil
		}
		err = fmt.Errorf("got %s quorum but it has invalid signatures: %w", phase, err)

		shares := len(container.GetSignatures(share.ValidatorIndex, root))
		b.FallBackAndVerifyEachSignature(container, root, share.Committee, share.ValidatorIndex)
		if hasQuorum, _ := container.HasQuorum(share.ValidatorIndex, root); !hasQuorum {
			return phase0.BLSSignature{}, recoverableReconstructError{err}
		}
		if len(container.GetSignatures(share.ValidatorIndex, root)) == shares {
			return phase0.BLSSignature{}, err
		}
	}
}

// reconstructedSig is a validator's signature, reconstructed from a quorum of its partial signatures.
type reconstructedSig struct {
	validatorIndex phase0.ValidatorIndex
	signature      phase0.BLSSignature
}

// reconstructPostConsensusSigs reconstructs the validators' signatures over root, a goroutine per validator, and
// returns the signatures and the reconstruction failures once every goroutine has returned. It leaves out the
// validators without a post-consensus quorum for root and those submitted reports as already submitted.
// rootsAhead are the roots the caller is yet to pass as root.
func (b *BaseRunner) reconstructPostConsensusSigs(
	logger *zap.Logger,
	span trace.Span,
	root [32]byte,
	rootsAhead [][32]byte,
	validators []phase0.ValidatorIndex,
	submitted func(phase0.ValidatorIndex) bool,
) ([]reconstructedSig, []error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex // guards sigs and errs
		sigs []reconstructedSig
		errs []error
	)
	for _, validatorIndex := range validators {
		// Re-check the quorum: the drops after a failed reconstruction (below) may have taken this root+validator
		// pair below quorum since basePostConsensusMsgProcessing reported it.
		gotQuorum, quorumSigners := b.State.PostConsensusContainer.HasQuorum(validatorIndex, root)
		if !gotQuorum || submitted(validatorIndex) {
			continue
		}

		wg.Go(func() {
			share := b.Share[validatorIndex]
			// Operators' views of a committee's validators may diverge (a ValidatorAdded event not seen yet, or
			// one that failed to process), so the share can be missing.
			if share == nil {
				return
			}
			pubKey := share.ValidatorPubKey

			vLogger := logger.With(
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.String("pubkey", hex.EncodeToString(pubKey[:])),
				fields.BlockRoot(root),
				zap.Uint64s("quorum_signers", quorumSigners),
			)

			// On failure the root's partial signatures have been verified and the invalid ones dropped; the
			// error is recoverable only if that left the root below quorum (see reconstructQuorumSig).
			sig, err := b.reconstructQuorumSig(b.State.PostConsensusContainer, root, share, "post-consensus")
			if err != nil {
				// Verify the validator's partial signatures for the roots still ahead too, dropping a bad
				// signer's shares before they cost a failed reconstruction each. No pair is verified by two
				// goroutines at once: the work is split by validator, and the roots are taken in turn.
				for _, root := range rootsAhead {
					b.FallBackAndVerifyEachSignature(b.State.PostConsensusContainer, root, share.Committee, validatorIndex)
				}
				const eventMsg = "got post-consensus quorum but it has invalid signatures"
				span.AddEvent(eventMsg)
				vLogger.Error(eventMsg, zap.Bool("recoverable", isRecoverableReconstructError(err)), zap.Error(err))

				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}

			vLogger.Debug("🧩 reconstructed partial signature")

			mu.Lock()
			sigs = append(sigs, reconstructedSig{validatorIndex: validatorIndex, signature: sig})
			mu.Unlock()
		})
	}
	wg.Wait()

	logger.Debug("🧩 reconstructed partial signatures for root", fields.BlockRoot(root))
	return sigs, errs
}
