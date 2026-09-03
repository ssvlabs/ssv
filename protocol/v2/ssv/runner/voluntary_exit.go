package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// VoluntaryExitRunner implements validator voluntary exit duty - this duty doesn't
// need consensus nor post-consensus, it just performs pre-consensus with VoluntaryExitPartialSig
// over a VoluntaryExit object to create a SignedVoluntaryExit
type VoluntaryExitRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	voluntaryExit *phase0.VoluntaryExit
}

// VoluntaryExitRunnerOptions bundles all dependencies required by NewVoluntaryExitRunner.
// It currently only embeds BaseRunnerOptions since the runner has no role-specific fields,
// but wrapping it keeps the constructor signature consistent with other runners.
type VoluntaryExitRunnerOptions struct {
	BaseRunnerOptions
}

func NewVoluntaryExitRunner(opts VoluntaryExitRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &VoluntaryExitRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleVoluntaryExit,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},

		beacon:         opts.Beacon,
		network:        opts.Network,
		signer:         opts.Signer,
		operatorSigner: opts.OperatorSigner,
	}, nil
}

func (r *VoluntaryExitRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

// ProcessPreConsensus Check for quorum of partial signatures over VoluntaryExit and,
// if has quorum, constructs SignedVoluntaryExit and submits to BeaconNode
func (r *VoluntaryExitRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	var validatorIndex phase0.ValidatorIndex
	if r.voluntaryExit != nil {
		validatorIndex = r.voluntaryExit.ValidatorIndex
		span.SetAttributes(observability.ValidatorIndexAttribute(validatorIndex))
	}

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing voluntary exit message: %w", err)
	}

	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], fullSig)

	// create SignedVoluntaryExit using VoluntaryExit created on r.executeDuty() and reconstructed signature
	signedVoluntaryExit := &phase0.SignedVoluntaryExit{
		Message:   r.voluntaryExit,
		Signature: specSig,
	}

	span.AddEvent("submitting voluntary exit")
	if err := r.beacon.SubmitVoluntaryExit(ctx, signedVoluntaryExit); err != nil {
		return fmt.Errorf("could not submit voluntary exit: %w", err)
	}

	const eventMsg = "✅ successfully submitted voluntary exit"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		fields.Epoch(r.voluntaryExit.Epoch),
		zap.Uint64("validator_index", uint64(validatorIndex)),
		zap.String("signature", hex.EncodeToString(specSig[:])),
	)

	r.markDutySucceeded()
	const dutyFinishedEvent = "✔️successfully finished duty processing"
	logger.Info(dutyFinishedEvent)
	span.AddEvent(dutyFinishedEvent)

	return nil
}

func (r *VoluntaryExitRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return spectypes.NewError(spectypes.ValidatorExitNoConsensusPhaseErrorCode, "no consensus phase for voluntary exit")
}

func (r *VoluntaryExitRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return spectypes.NewError(spectypes.ValidatorExitNoPostConsensusPhaseErrorCode, "no post consensus phase for voluntary exit")
}

func (r *VoluntaryExitRunner) expectedPreConsensusRootsAndDomain() ([]spectypes.HashRoot, phase0.DomainType, error) {
	validatorDuty, err := r.currentValidatorDuty()
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("current validator duty: %w", err)
	}

	vr, err := r.calculateVoluntaryExit(validatorDuty)
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("could not calculate voluntary exit: %w", err)
	}
	return []spectypes.HashRoot{vr}, spectypes.DomainVoluntaryExit, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *VoluntaryExitRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]spectypes.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no post consensus roots for voluntary exit")
}

func (r *VoluntaryExitRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	voluntaryExit, err := r.calculateVoluntaryExit(validatorDuty)
	if err != nil {
		return fmt.Errorf("could not calculate voluntary exit: %w", err)
	}

	// get PartialSignatureMessage with voluntaryExit root and signature
	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		validatorDuty,
		voluntaryExit,
		validatorDuty.DutySlot(),
		spectypes.DomainVoluntaryExit,
	)
	if err != nil {
		// EIP-7044 pins exits to the Capella domain, so a signer that disagrees on the fork
		// rejects exits while other duties still sign (e.g. a remote Web3Signer with the wrong --network).
		return fmt.Errorf("could not sign voluntary exit (if signing remotely, check the signer config, e.g. the Web3Signer --network): %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.VoluntaryExitPartialSig,
		Slot:     validatorDuty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	logger.Debug("signing and broadcasting voluntary exit partial sig", fields.Slot(duty.DutySlot()))

	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey, msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast voluntary exit partial sig: %w", err)
	}

	// stores value for later using in ProcessPreConsensus
	r.voluntaryExit = voluntaryExit

	return nil
}

// Returns *phase0.VoluntaryExit object with current epoch and own validator index
func (r *VoluntaryExitRunner) calculateVoluntaryExit(duty *spectypes.ValidatorDuty) (*phase0.VoluntaryExit, error) {
	if duty == nil {
		return nil, fmt.Errorf("validator duty is nil")
	}

	return &phase0.VoluntaryExit{
		Epoch:          r.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot()),
		ValidatorIndex: duty.ValidatorIndex,
	}, nil
}

func (r *VoluntaryExitRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *VoluntaryExitRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *VoluntaryExitRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *VoluntaryExitRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *VoluntaryExitRunner) MarshalJSON() ([]byte, error) {
	type voluntaryExitRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}

	return json.Marshal(&voluntaryExitRunnerJSON{
		BaseRunner: r.BaseRunner,
	})
}

func (r *VoluntaryExitRunner) UnmarshalJSON(data []byte) error {
	type voluntaryExitRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}

	aux := &voluntaryExitRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}

	r.BaseRunner = aux.BaseRunner
	return nil
}

// Encode returns the encoded struct in bytes or error
func (r *VoluntaryExitRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *VoluntaryExitRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

// GetRoot returns the root used for signing and verification
func (r *VoluntaryExitRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode VoluntaryExitRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
