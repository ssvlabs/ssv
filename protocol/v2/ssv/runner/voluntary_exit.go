package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// VoluntaryExitRunner implements validator voluntary exit duty - this duty doesn't
// need consensus nor post-consensus, it just performs pre-consensus with VoluntaryExitPartialSig
// over a VoluntaryExit object to create a SignedVoluntaryExit
type VoluntaryExitRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	voluntaryExit *phase0.VoluntaryExit
}

func NewVoluntaryExitRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &VoluntaryExitRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleVoluntaryExit,
			NetworkConfig:  networkConfig,
			Share:          share,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
	}, nil
}

func (r *VoluntaryExitRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewNonBeaconDuty(ctx, logger, r, duty.(*spectypes.ValidatorDuty), quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *VoluntaryExitRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

// ProcessPreConsensus Check for quorum of partial signatures over VoluntaryExit and,
// if has quorum, constructs SignedVoluntaryExit and submits to BeaconNode
func (r *VoluntaryExitRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_pre_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	var validatorIndex phase0.ValidatorIndex
	if r.voluntaryExit != nil {
		validatorIndex = r.voluntaryExit.ValidatorIndex
		span.SetAttributes(observability.ValidatorIndexAttribute(validatorIndex))
	}

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing voluntary exit message: %w", err)
	}

	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return traces.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
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
		return traces.Errorf(span, "could not submit voluntary exit: %w", err)
	}

	const eventMsg = "✅ successfully submitted voluntary exit"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		fields.Epoch(r.voluntaryExit.Epoch),
		zap.Uint64("validator_index", uint64(validatorIndex)),
		zap.String("signature", hex.EncodeToString(specSig[:])),
	)

	r.GetState().Finished = true

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *VoluntaryExitRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return spectypes.NewError(spectypes.ValidatorExitNoConsensusPhaseErrorCode, "no consensus phase for voluntary exit")
}

func (r *VoluntaryExitRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, msg)
}

func (r *VoluntaryExitRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return spectypes.NewError(spectypes.ValidatorExitNoPostConsensusPhaseErrorCode, "no post consensus phase for voluntary exit")
}

func (r *VoluntaryExitRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	vr, err := r.calculateVoluntaryExit()
	if err != nil {
		return nil, spectypes.DomainError, errors.Wrap(err, "could not calculate voluntary exit")
	}
	return []ssz.HashRoot{vr}, spectypes.DomainVoluntaryExit, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *VoluntaryExitRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for voluntary exit")
}

func (r *VoluntaryExitRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	voluntaryExit, err := r.calculateVoluntaryExit()
	if err != nil {
		return traces.Errorf(span, "could not calculate voluntary exit: %w", err)
	}

	// get PartialSignatureMessage with voluntaryExit root and signature
	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		duty.(*spectypes.ValidatorDuty),
		voluntaryExit,
		duty.DutySlot(),
		spectypes.DomainVoluntaryExit,
	)
	if err != nil {
		return traces.Errorf(span, "could not sign VoluntaryExit object: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.VoluntaryExitPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode PartialSignatureMessages: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting signed SSV message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast signedPartialMsg with VoluntaryExit: %w", err)
	}

	// stores value for later using in ProcessPreConsensus
	r.voluntaryExit = voluntaryExit

	span.SetStatus(codes.Ok, "")
	return nil
}

// Returns *phase0.VoluntaryExit object with current epoch and own validator index
func (r *VoluntaryExitRunner) calculateVoluntaryExit() (*phase0.VoluntaryExit, error) {
	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.BaseRunner.State.CurrentDuty.DutySlot())
	validatorIndex := r.GetState().CurrentDuty.(*spectypes.ValidatorDuty).ValidatorIndex
	return &phase0.VoluntaryExit{
		Epoch:          epoch,
		ValidatorIndex: validatorIndex,
	}, nil
}

func (r *VoluntaryExitRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *VoluntaryExitRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *VoluntaryExitRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *VoluntaryExitRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *VoluntaryExitRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *VoluntaryExitRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *VoluntaryExitRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *VoluntaryExitRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
}

func (r *VoluntaryExitRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *VoluntaryExitRunner) GetNetworkConfig() *networkconfig.Network {
	return r.BaseRunner.NetworkConfig
}

func (r *VoluntaryExitRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *VoluntaryExitRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}
func (r *VoluntaryExitRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *VoluntaryExitRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *VoluntaryExitRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}
func (r *VoluntaryExitRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *VoluntaryExitRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *VoluntaryExitRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *VoluntaryExitRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode VoluntaryExitRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
