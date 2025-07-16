package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type ValidatorRegistrationRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       ssv.ValueChecker

	gasLimit uint64
}

func NewValidatorRegistrationRunner(
	networkConfig networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	gasLimit uint64,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &ValidatorRegistrationRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleValidatorRegistration,
			NetworkConfig:  networkConfig,
			Share:          share,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		gasLimit:       gasLimit,
	}, nil
}

func (r *ValidatorRegistrationRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewNonBeaconDuty(ctx, logger, r, duty.(*spectypes.ValidatorDuty), quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *ValidatorRegistrationRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *ValidatorRegistrationRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_pre_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
			observability.ValidatorPublicKeyAttribute(phase0.BLSPubKey(r.GetShare().ValidatorPubKey)),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return observability.Errorf(span, "failed processing validator registration message: %w", err)
	}

	// TODO: (Alan) revert
	logger.Debug("got partial sig",
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Bool("quorum", hasQuorum))

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
		return observability.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], fullSig)

	share := r.GetShare()
	if share == nil {
		return observability.Errorf(span, "no share to get validator public key: %w", err)
	}

	registration, err := r.calculateValidatorRegistration(r.BaseRunner.State.StartingDuty.DutySlot())
	if err != nil {
		return observability.Errorf(span, "could not calculate validator registration: %w", err)
	}

	signedRegistration := &api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &v1.SignedValidatorRegistration{
			Message:   registration,
			Signature: specSig,
		},
	}

	span.AddEvent("submitting validator registration")
	if err := r.beacon.SubmitValidatorRegistration(signedRegistration); err != nil {
		return observability.Errorf(span, "could not submit validator registration: %w", err)
	}

	const eventMsg = "validator registration submitted successfully"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		fields.FeeRecipient(share.FeeRecipientAddress[:]),
		zap.String("signature", hex.EncodeToString(specSig[:])))

	r.GetState().Finished = true

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *ValidatorRegistrationRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.BaseRunner.State == nil || r.BaseRunner.State.StartingDuty == nil {
		return nil, spectypes.DomainError, errors.New("no running duty to compute preconsensus roots and domain")
	}
	vr, err := r.calculateValidatorRegistration(r.BaseRunner.State.StartingDuty.DutySlot())
	if err != nil {
		return nil, spectypes.DomainError, errors.Wrap(err, "could not calculate validator registration")
	}
	return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ValidatorRegistrationRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for validator registration")
}

func (r *ValidatorRegistrationRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	vr, err := r.calculateValidatorRegistration(duty.DutySlot())
	if err != nil {
		return observability.Errorf(span, "could not calculate validator registration: %w", err)
	}

	// sign partial randao
	span.AddEvent("signing beacon object")
	msg, err := r.BaseRunner.signBeaconObject(
		ctx,
		r,
		duty.(*spectypes.ValidatorDuty),
		vr,
		duty.DutySlot(),
		spectypes.DomainApplicationBuilder,
	)
	if err != nil {
		return observability.Errorf(span, "could not sign validator registration: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ValidatorRegistrationPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.GetDomainType(), r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return observability.Errorf(span, "could not encode validator registration partial sig message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return observability.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	logger.Debug(
		"broadcasting validator registration partial sig",
		fields.Slot(duty.DutySlot()),
		zap.Any("validator_registration", vr),
	)

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return observability.Errorf(span, "can't broadcast partial randao sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *ValidatorRegistrationRunner) calculateValidatorRegistration(slot phase0.Slot) (*v1.ValidatorRegistration, error) {
	share := r.GetShare()
	if share == nil {
		return nil, errors.New("no share to get validator public key")
	}

	pk := phase0.BLSPubKey{}
	copy(pk[:], share.ValidatorPubKey[:])

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(slot)

	return &v1.ValidatorRegistration{
		FeeRecipient: share.FeeRecipientAddress,
		GasLimit:     r.gasLimit,
		Timestamp:    r.BaseRunner.NetworkConfig.EpochStartTime(epoch),
		Pubkey:       pk,
	}, nil
}

func (r *ValidatorRegistrationRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *ValidatorRegistrationRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *ValidatorRegistrationRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ValidatorRegistrationRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *ValidatorRegistrationRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *ValidatorRegistrationRunner) GetValChecker() ssv.ValueChecker {
	return r.valCheck
}

func (r *ValidatorRegistrationRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}
func (r *ValidatorRegistrationRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *ValidatorRegistrationRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ValidatorRegistrationRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *ValidatorRegistrationRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode ValidatorRegistrationRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
