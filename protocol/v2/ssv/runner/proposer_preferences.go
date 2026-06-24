package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

var _ Runner = (*ProposerPreferencesRunner)(nil)

// ProposerPreferencesRunner runs the Gloas (ePBS) proposer-preferences duty (SIP #94 §5): one duty
// per upcoming proposal slot, broadcasting the fee recipient and target gas limit builders must
// honor. Like the PTC runner it has no consensus or pre-consensus negotiation — each operator builds
// the preference from its own beacon node and a per-validator signature reconstructs only once a
// threshold of operators converged on byte-identical preferences (honest convergence, not consensus).
//
// duty.Slot is the proposal slot the preference targets, which is also the slot carried on the
// partial-signature message and the slot whose epoch fixes the signing domain — keeping all three in
// step with the base runner's slot checks. The duty executes (emits) earlier, near the current slot,
// so the message rides a future slot; permitting that is the message-validation layer's job, not the
// runner's.
type ProposerPreferencesRunner struct {
	*BaseRunner

	beacon               beacon.BeaconNode
	network              protocolp2p.Network
	signer               ekm.BeaconSigner
	operatorSigner       ssvtypes.OperatorSigner
	feeRecipientProvider feeRecipientProvider
	gasLimit             uint64

	// proposerPreferences is the operator's frozen observation for this proposal slot: the preference
	// it built (including the dependent_root its own beacon node reported). Incoming partial signatures
	// are validated and aggregated against exactly this object's signing root; nil means the duty has
	// not executed yet.
	proposerPreferences *gloas.ProposerPreferences
}

// ProposerPreferencesRunnerOptions bundles the dependencies required by NewProposerPreferencesRunner.
type ProposerPreferencesRunnerOptions struct {
	BaseRunnerOptions

	FeeRecipientProvider feeRecipientProvider
	GasLimit             uint64
}

func NewProposerPreferencesRunner(opts ProposerPreferencesRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, fmt.Errorf("must have one share")
	}

	return &ProposerPreferencesRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleProposerPreferences,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},

		beacon:               opts.Beacon,
		network:              opts.Network,
		signer:               opts.Signer,
		operatorSigner:       opts.OperatorSigner,
		feeRecipientProvider: opts.FeeRecipientProvider,
		gasLimit:             opts.GasLimit,
	}, nil
}

func (r *ProposerPreferencesRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	// Clear any prior observation; executeDuty re-freezes it, so a not-yet-executed duty stays nil.
	r.proposerPreferences = nil
	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *ProposerPreferencesRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// The runner is reused across duties, so a late message for a concluded duty is retryable.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing proposer preferences message: %w", err)
	}

	// quorum returns true only once (the first time it is reached).
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here; the quorum fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	if r.proposerPreferences == nil {
		return fmt.Errorf("reached quorum without frozen proposer preferences")
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature is invalid, surface which partial signatures were at fault.
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}
	var signature phase0.BLSSignature
	copy(signature[:], fullSig)

	signed := &gloas.SignedProposerPreferences{
		Message:   r.proposerPreferences,
		Signature: signature,
	}
	if err := r.beacon.SubmitProposerPreferences(ctx, []*gloas.SignedProposerPreferences{signed}); err != nil {
		if errors.Is(err, gloas.ErrProposerPreferencesPublishUnavailable) {
			// We converged and reconstructed correctly; there is simply no upstream endpoint to publish
			// to yet (SIP #94 §5). Record a benign no-op rather than a failure so the known-missing
			// endpoint doesn't surface as operator-actionable errors. Returning nil leaves the deferred
			// err nil, so markDutyFailed does not also fire.
			logger.Debug("proposer preferences reconstructed but publish endpoint unavailable; skipping submit", fields.Slot(r.proposerPreferences.ProposalSlot))
			r.markDutyNotRequired()
			return nil
		}
		return fmt.Errorf("could not submit proposer preferences: %w", err)
	}

	r.markDutySucceeded()
	logger.Info("✔️ successfully submitted proposer preferences", fields.Slot(r.proposerPreferences.ProposalSlot))
	return nil
}

func (r *ProposerPreferencesRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return fmt.Errorf("no consensus phase for proposer preferences")
}

func (r *ProposerPreferencesRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return fmt.Errorf("no post-consensus phase for proposer preferences")
}

func (r *ProposerPreferencesRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.proposerPreferences == nil {
		return nil, spectypes.DomainError, fmt.Errorf("no frozen proposer preferences")
	}
	return []ssz.HashRoot{r.proposerPreferences}, phase0.DomainType(spectypes.DomainProposerPreferences), nil
}

func (r *ProposerPreferencesRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, fmt.Errorf("no post-consensus roots for proposer preferences")
}

func (r *ProposerPreferencesRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	proposalSlot := validatorDuty.DutySlot()

	preferences, err := r.buildProposerPreferences(ctx, proposalSlot)
	if err != nil {
		// Building hits the beacon node (dependent-root fetch) and validator config (fee recipient);
		// a failure there is operational, so record a failed duty to surface it in metrics.
		logger.Warn("proposer preferences failed: could not build preferences", fields.Slot(proposalSlot), zap.Error(err))
		r.markDutyFailed(err)
		return nil
	}

	// Freeze the observation: peers' partial signatures are validated and aggregated against exactly
	// this object's signing root, so only operators that converged on identical preferences (same
	// dependent_root, fee recipient, gas limit) reach quorum.
	r.proposerPreferences = preferences

	msg, err := signBeaconObject(ctx, r, r.NetworkConfig, validatorDuty, preferences, proposalSlot, phase0.DomainType(spectypes.DomainProposerPreferences))
	if err != nil {
		return fmt.Errorf("could not sign proposer preferences: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ProposerPreferencesPartialSig,
		Slot:     proposalSlot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast proposer preferences partial sig: %w", err)
	}
	return nil
}

// buildProposerPreferences assembles the preference for the proposal slot from this operator's own
// view: fee recipient and target gas limit from validator config (matching validator registration),
// and the dependent_root of the proposer duties for the proposal slot's epoch — the seed that fixed
// this proposal assignment (SIP #94 §5), fetched per-operator so convergence is over identical roots.
func (r *ProposerPreferencesRunner) buildProposerPreferences(ctx context.Context, proposalSlot phase0.Slot) (*gloas.ProposerPreferences, error) {
	validatorPubKey := r.GetShare().ValidatorPubKey

	feeRecipient, err := r.feeRecipientProvider.GetFeeRecipient(validatorPubKey)
	if err != nil {
		return nil, fmt.Errorf("could not get fee recipient for validator %x: %w", validatorPubKey, err)
	}

	gasLimit := r.gasLimit
	if gasLimit == 0 {
		gasLimit = DefaultGasLimit
	}

	epoch := r.NetworkConfig.EstimatedEpochAtSlot(proposalSlot)
	dependentRoot, err := r.beacon.ProposerDutiesDependentRoot(ctx, epoch)
	if err != nil {
		return nil, fmt.Errorf("could not fetch proposer-duties dependent root for epoch %d: %w", epoch, err)
	}

	return &gloas.ProposerPreferences{
		DependentRoot:  dependentRoot,
		ProposalSlot:   proposalSlot,
		ValidatorIndex: r.GetShare().ValidatorIndex,
		FeeRecipient:   feeRecipient,
		TargetGasLimit: gasLimit,
	}, nil
}

func (r *ProposerPreferencesRunner) GetNetwork() protocolp2p.Network { return r.network }

func (r *ProposerPreferencesRunner) GetBeaconNode() beacon.BeaconNode { return r.beacon }

func (r *ProposerPreferencesRunner) GetSigner() ekm.BeaconSigner { return r.signer }

func (r *ProposerPreferencesRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Only BaseRunner is persisted; the frozen observation is transient per-duty state.
func (r *ProposerPreferencesRunner) MarshalJSON() ([]byte, error) {
	type proposerPreferencesRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	return json.Marshal(&proposerPreferencesRunnerJSON{BaseRunner: r.BaseRunner})
}

func (r *ProposerPreferencesRunner) UnmarshalJSON(data []byte) error {
	type proposerPreferencesRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	aux := &proposerPreferencesRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}
	r.BaseRunner = aux.BaseRunner
	return nil
}

func (r *ProposerPreferencesRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *ProposerPreferencesRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

func (r *ProposerPreferencesRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode ProposerPreferencesRunner: %w", err)
	}
	return sha256.Sum256(marshaledRoot), nil
}
