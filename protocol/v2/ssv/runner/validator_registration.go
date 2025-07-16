package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cespare/xxhash/v2"
	"github.com/ethereum/go-ethereum/common"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type ValidatorRegistrationRunner struct {
	BaseRunner *BaseRunner

	beacon                         beacon.BeaconNode
	network                        specqbft.Network
	signer                         ekm.BeaconSigner
	operatorSigner                 ssvtypes.OperatorSigner
	valCheck                       specqbft.ProposedValueCheckF
	recipientsStorage              recipientsStorage
	validatorRegistrationSubmitter ValidatorRegistrationSubmitter
	validatorOwnerAddress          common.Address

	gasLimit uint64
}

func NewValidatorRegistrationRunner(
	networkConfig networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	validatorOwnerAddress common.Address,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	recipientsStorage recipientsStorage,
	ValidatorRegistrationSubmitter ValidatorRegistrationSubmitter,
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

		beacon:                         beacon,
		network:                        network,
		signer:                         signer,
		operatorSigner:                 operatorSigner,
		recipientsStorage:              recipientsStorage,
		validatorRegistrationSubmitter: ValidatorRegistrationSubmitter,
		validatorOwnerAddress:          validatorOwnerAddress,

		gasLimit: gasLimit,
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
	if err := r.validatorRegistrationSubmitter.Enqueue(signedRegistration); err != nil {
		return observability.Errorf(span, "could not submit validator registration: %w", err)
	}

	const eventMsg = "validator registration submitted successfully"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		fields.FeeRecipient(registration.FeeRecipient[:]),
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
	pk := phase0.BLSPubKey{}
	copy(pk[:], r.GetShare().ValidatorPubKey[:])

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(slot)

	rData, found, err := r.recipientsStorage.GetRecipientData(nil, r.validatorOwnerAddress)
	if err != nil {
		return nil, fmt.Errorf("get recipient data from storage: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("recipient data not found for owner %s", r.validatorOwnerAddress.Hex())
	}

	return &v1.ValidatorRegistration{
		FeeRecipient: rData.FeeRecipient,
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

func (r *ValidatorRegistrationRunner) GetValCheckF() specqbft.ProposedValueCheckF {
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

type recipientsStorage interface {
	GetRecipientData(r basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error)
}

type ValidatorRegistrationSubmitter interface {
	Enqueue(registration *api.VersionedSignedValidatorRegistration) error
}

type VRSubmitter struct {
	logger *zap.Logger

	beaconConfig   *networkconfig.BeaconConfig
	beacon         beacon.BeaconNode
	validatorStore validatorStore

	// registrationMu synchronises access to registrations
	registrationMu sync.Mutex
	// registrations is a set of validator-registrations (their latest versions) to be sent to
	// Beacon node to ensure various entities in Ethereum network, such as Relays, are aware of
	// participating validators and their chosen preferences (gas limit, fee recipient, etc.)
	registrations map[phase0.BLSPubKey]*validatorRegistration
}

func NewVRSubmitter(
	ctx context.Context,
	logger *zap.Logger,
	beaconConfig *networkconfig.BeaconConfig,
	beacon beacon.BeaconNode,
	validatorStore validatorStore,
) *VRSubmitter {
	submitter := &VRSubmitter{
		logger:         logger,
		beaconConfig:   beaconConfig,
		beacon:         beacon,
		validatorStore: validatorStore,
		registrations:  map[phase0.BLSPubKey]*validatorRegistration{},
	}

	slotTicker := slotticker.New(logger, slotticker.Config{
		SlotDuration: beaconConfig.SlotDuration,
		GenesisTime:  beaconConfig.GenesisTime,
	})
	go submitter.start(ctx, slotTicker)

	return submitter
}

// start periodically submits validator registrations of 2 types (in batches, 1 batch per slot):
// - new validator registrations
// - validator registrations that are relevant for the near future (targeting 10th epoch from now)
// This allows us to keep the amount of registration submissions small and not having to worry
// about pruning gc.registrations "cache" (since it might contain registrations for validators that
// are no longer operating) while still submitting all validator-registrations that matter asap.
func (s *VRSubmitter) start(ctx context.Context, ticker slotticker.SlotTicker) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			config := s.beaconConfig

			currentSlot := ticker.Slot()
			currentEpoch := config.EstimatedEpochAtSlot(currentSlot)
			slotInEpoch := uint64(currentSlot) % config.SlotsPerEpoch

			// Select registrations to submit.
			targetRegs := make(map[phase0.BLSPubKey]*validatorRegistration, 0)
			s.registrationMu.Lock()
			// 1. find and add validators attesting in the 10th epoch from now
			shares := s.validatorStore.SelfValidators()
			for _, share := range shares {
				if !share.IsParticipatingAndAttesting(currentEpoch + 10) {
					continue
				}
				pk := phase0.BLSPubKey{}
				copy(pk[:], share.ValidatorPubKey[:])
				r, ok := s.registrations[pk]
				if !ok {
					// we haven't constructed the corresponding validator registration for submission yet,
					// so skip it for now
					continue
				}
				targetRegs[pk] = r
			}
			// 2. find and add newly created validator registrations
			for pk, r := range s.registrations {
				if r.new {
					targetRegs[pk] = r
				}
			}
			s.registrationMu.Unlock()

			registrations := make([]*api.VersionedSignedValidatorRegistration, 0)
			for _, r := range targetRegs {
				validatorPk, err := r.PubKey()
				if err != nil {
					s.logger.Error("Failed to get validator pubkey", zap.Error(err), fields.Slot(currentSlot))
					continue
				}

				// Distribute the registrations evenly across the epoch based on the pubkeys.
				validatorDescriptor := xxhash.Sum64(validatorPk[:])
				shouldSubmit := validatorDescriptor%config.SlotsPerEpoch == slotInEpoch

				if r.new || shouldSubmit {
					r.new = false
					registrations = append(registrations, r.VersionedSignedValidatorRegistration)
				}
			}

			err := s.beacon.SubmitValidatorRegistrations(ctx, registrations)
			if err != nil {
				s.logger.Error(
					"Failed to submit validator registrations",
					zap.Int("registrations", len(registrations)),
					zap.Error(err),
				)
			}
		}
	}
}

// Enqueue enqueues new validator registration for submission, the submission happens asynchronously
// in a batch with other validator registrations. If validator registration already exists it is
// replaced by this new one.
func (s *VRSubmitter) Enqueue(registration *api.VersionedSignedValidatorRegistration) error {
	pk, err := registration.PubKey()
	if err != nil {
		return err
	}

	s.registrationMu.Lock()
	defer s.registrationMu.Unlock()

	s.registrations[pk] = &validatorRegistration{
		VersionedSignedValidatorRegistration: registration,
		new:                                  true,
	}

	return nil
}

type validatorRegistration struct {
	*api.VersionedSignedValidatorRegistration

	// new signifies whether this validator registration has already been submitted previously.
	new bool
}

type validatorStore interface {
	SelfValidators() []*ssvtypes.SSVShare
}
