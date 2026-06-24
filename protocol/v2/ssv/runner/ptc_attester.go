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

var _ Runner = (*PTCAttesterRunner)(nil)

// PTCAttesterRunner runs the Gloas (ePBS) Payload Timeliness Committee attestation duty (SIP #94 §3).
// It has no consensus or pre-consensus negotiation: each operator signs the PayloadAttestationData its
// own beacon node reports at execution time, and a per-validator signature reconstructs only once a
// threshold of operators converged on byte-identical data — honest convergence, not consensus.
type PTCAttesterRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	// payloadAttestationData is the operator's frozen observation. Incoming partial signatures are
	// validated and aggregated against exactly this root; nil means the operator abstained (saw no
	// block) and is sitting the duty out.
	payloadAttestationData *gloas.PayloadAttestationData
}

// PTCAttesterRunnerOptions bundles the dependencies required by NewPTCAttesterRunner.
type PTCAttesterRunnerOptions struct {
	BaseRunnerOptions
}

func NewPTCAttesterRunner(opts PTCAttesterRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, fmt.Errorf("must have one share")
	}

	return &PTCAttesterRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RolePTCAttester,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},

		beacon:         opts.Beacon,
		network:        opts.Network,
		signer:         opts.Signer,
		operatorSigner: opts.OperatorSigner,
	}, nil
}

func (r *PTCAttesterRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	// Clear any prior observation; executeDuty re-freezes it only if this operator attests, so an
	// abstained or not-yet-executed duty stays nil.
	r.payloadAttestationData = nil
	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *PTCAttesterRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// The runner is reused across duties, so a late message for a finished duty is retryable.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing payload attestation message: %w", err)
	}

	// quorum returns true only once (the first time it is reached).
	if !hasQuorum {
		return nil
	}

	if r.payloadAttestationData == nil {
		return fmt.Errorf("reached quorum without a frozen payload attestation data")
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

	msg := &gloas.PayloadAttestationMessage{
		ValidatorIndex: r.GetShare().ValidatorIndex,
		Data:           r.payloadAttestationData,
		Signature:      signature,
	}
	if err := r.beacon.SubmitPayloadAttestationMessages(ctx, []*gloas.PayloadAttestationMessage{msg}); err != nil {
		return fmt.Errorf("could not submit payload attestation message: %w", err)
	}

	r.markDutySucceeded()
	logger.Info("✔️ successfully submitted payload attestation", fields.Slot(r.payloadAttestationData.Slot))
	return nil
}

func (r *PTCAttesterRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return fmt.Errorf("no consensus phase for PTC attestation")
}

func (r *PTCAttesterRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return fmt.Errorf("no post-consensus phase for PTC attestation")
}

func (r *PTCAttesterRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.payloadAttestationData == nil {
		return nil, spectypes.DomainError, fmt.Errorf("no frozen payload attestation data")
	}
	return []ssz.HashRoot{r.payloadAttestationData}, phase0.DomainType(spectypes.DomainPTCAttester), nil
}

func (r *PTCAttesterRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, fmt.Errorf("no post-consensus roots for PTC attestation")
}

func (r *PTCAttesterRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	slot := validatorDuty.DutySlot()

	// Observe the slot's payload-attestation data from our own beacon node. Per SIP #94 §3, an
	// operator that has seen no beacon block for the slot abstains (signs and submits nothing).
	data, err := r.beacon.PayloadAttestationData(ctx, slot)
	if err != nil {
		// A beacon-node failure (syncing, auth, unreachable) forces an abstain but is operational,
		// not the normal "saw no block" case below — surface it so it isn't silently dropped.
		logger.Warn("abstaining from PTC attestation: failed to fetch payload attestation data", fields.Slot(slot), zap.Error(err))
		r.markDutySucceeded()
		return nil
	}
	if data.BeaconBlockRoot == (phase0.Root{}) {
		logger.Debug("abstaining from PTC attestation: no beacon block for slot", fields.Slot(slot))
		r.markDutySucceeded()
		return nil
	}

	// Freeze the observation: peers' partial signatures are validated and aggregated against exactly
	// this root, so only operators that converged on identical data reach quorum.
	r.payloadAttestationData = data

	msg, err := signBeaconObject(ctx, r, r.NetworkConfig, validatorDuty, data, slot, phase0.DomainType(spectypes.DomainPTCAttester))
	if err != nil {
		return fmt.Errorf("could not sign payload attestation data: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PTCAttesterPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast payload attestation partial sig: %w", err)
	}
	return nil
}

func (r *PTCAttesterRunner) GetNetwork() protocolp2p.Network { return r.network }

func (r *PTCAttesterRunner) GetBeaconNode() beacon.BeaconNode { return r.beacon }

func (r *PTCAttesterRunner) GetSigner() ekm.BeaconSigner { return r.signer }

func (r *PTCAttesterRunner) GetOperatorSigner() ssvtypes.OperatorSigner { return r.operatorSigner }

// Only BaseRunner is persisted; the frozen observation is transient per-duty state.
func (r *PTCAttesterRunner) MarshalJSON() ([]byte, error) {
	type ptcAttesterRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	return json.Marshal(&ptcAttesterRunnerJSON{BaseRunner: r.BaseRunner})
}

func (r *PTCAttesterRunner) UnmarshalJSON(data []byte) error {
	type ptcAttesterRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	aux := &ptcAttesterRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}
	r.BaseRunner = aux.BaseRunner
	return nil
}

func (r *PTCAttesterRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *PTCAttesterRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

func (r *PTCAttesterRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode PTCAttesterRunner: %w", err)
	}
	return sha256.Sum256(marshaledRoot), nil
}
