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
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cespare/xxhash/v2"
	ssz "github.com/ferranbt/fastssz"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	DefaultGasLimit    = uint64(36_000_000)
	DefaultGasLimitOld = uint64(30_000_000)
)

type ValidatorRegistrationRunner struct {
	BaseRunner *BaseRunner

	beacon                         beacon.BeaconNode
	network                        specqbft.Network
	signer                         ekm.BeaconSigner
	operatorSigner                 ssvtypes.OperatorSigner
	validatorRegistrationSubmitter ValidatorRegistrationSubmitter
	feeRecipientProvider           feeRecipientProvider

	gasLimit uint64
}

func NewValidatorRegistrationRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	validatorRegistrationSubmitter ValidatorRegistrationSubmitter,
	feeRecipientProvider feeRecipientProvider,
	gasLimit uint64,
) (Runner, error) {
	if len(share) != 1 {
		return nil, fmt.Errorf("must have one share")
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
		validatorRegistrationSubmitter: validatorRegistrationSubmitter,
		feeRecipientProvider:           feeRecipientProvider,

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
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return fmt.Errorf("failed processing validator registration message: %w", err)
	}

	logger.Debug("got partial sig",
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Bool("quorum", hasQuorum))

	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		return nil
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]

	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], fullSig)

	registration, err := r.buildValidatorRegistration(r.BaseRunner.State.CurrentDuty.DutySlot())
	if err != nil {
		return fmt.Errorf("could not calculate validator registration: %w", err)
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
		return fmt.Errorf("could not submit validator registration: %w", err)
	}

	const eventMsg = "validator registration submitted successfully"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		fields.FeeRecipient(registration.FeeRecipient[:]),
		zap.String("signature", hex.EncodeToString(specSig[:])))

	r.GetState().Finished = true

	return nil
}

func (r *ValidatorRegistrationRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return spectypes.NewError(spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode, "no consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, msg)
}

func (r *ValidatorRegistrationRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return spectypes.NewError(spectypes.ValidatorRegistrationNoPostConsensusPhaseErrorCode, "no post consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.BaseRunner.State == nil || r.BaseRunner.State.CurrentDuty == nil {
		return nil, spectypes.DomainError, fmt.Errorf("no running duty to compute preconsensus roots and domain")
	}
	vr, err := r.buildValidatorRegistration(r.BaseRunner.State.CurrentDuty.DutySlot())
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("could not calculate validator registration: %w", err)
	}
	return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ValidatorRegistrationRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, fmt.Errorf("no post consensus roots for validator registration")
}

func (r *ValidatorRegistrationRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	vr, err := r.buildValidatorRegistration(duty.DutySlot())
	if err != nil {
		return fmt.Errorf("could not calculate validator registration: %w", err)
	}

	// sign partial randao
	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		duty.(*spectypes.ValidatorDuty),
		vr,
		duty.DutySlot(),
		spectypes.DomainApplicationBuilder,
	)
	if err != nil {
		return fmt.Errorf("could not sign validator registration: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ValidatorRegistrationPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return fmt.Errorf("could not encode validator registration partial sig message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	logger.Debug("broadcasting validator registration partial sig", zap.Any("validator_registration", vr))

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return fmt.Errorf("can't broadcast partial randao sig: %w", err)
	}

	return nil
}

func (r *ValidatorRegistrationRunner) buildValidatorRegistration(slot phase0.Slot) (*v1.ValidatorRegistration, error) {
	validatorPubKey := r.GetShare().ValidatorPubKey

	feeRecipient, err := r.feeRecipientProvider.GetFeeRecipient(validatorPubKey)
	if err != nil {
		return nil, fmt.Errorf("could not get fee recipient for validator %x: %w", validatorPubKey, err)
	}

	// Set the default GasLimit value if it hasn't been specified already, use 36 or 30 depending
	// on the current epoch as compared to when this transition is supposed to happen.
	gasLimit := r.gasLimit
	if gasLimit == 0 {
		defaultGasLimit := DefaultGasLimit
		if !r.BaseRunner.NetworkConfig.GasLimit36Fork() {
			defaultGasLimit = DefaultGasLimitOld
		}
		gasLimit = defaultGasLimit
	}

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(slot)
	return &v1.ValidatorRegistration{
		FeeRecipient: feeRecipient,
		GasLimit:     gasLimit,
		Timestamp:    r.BaseRunner.NetworkConfig.EpochStartTime(epoch),
		Pubkey:       phase0.BLSPubKey(validatorPubKey),
	}, nil
}

func (r *ValidatorRegistrationRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *ValidatorRegistrationRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *ValidatorRegistrationRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *ValidatorRegistrationRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *ValidatorRegistrationRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *ValidatorRegistrationRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *ValidatorRegistrationRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *ValidatorRegistrationRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
}

func (r *ValidatorRegistrationRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *ValidatorRegistrationRunner) GetNetworkConfig() *networkconfig.Network {
	return r.BaseRunner.NetworkConfig
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
		return [32]byte{}, fmt.Errorf("could not encode ValidatorRegistrationRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

type ValidatorRegistrationSubmitter interface {
	Enqueue(registration *api.VersionedSignedValidatorRegistration) error
}

type VRSubmitter struct {
	logger *zap.Logger

	beaconConfig   *networkconfig.Beacon
	beacon         beacon.BeaconNode
	validatorStore validatorStore

	// registrationMu synchronizes access to registrations
	registrationMu sync.Mutex
	// registrations is a set of validator-registrations (their latest versions) to be sent to
	// Beacon node to ensure various entities in Ethereum network, such as Relays, are aware of
	// participating validators and their chosen preferences (gas limit, fee recipient, etc.)
	registrations map[phase0.BLSPubKey]*validatorRegistration
}

func NewVRSubmitter(
	ctx context.Context,
	logger *zap.Logger,
	beaconConfig *networkconfig.Beacon,
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
				if !share.IsAttesting(currentEpoch + 10) {
					continue
				}
				pk := phase0.BLSPubKey(share.ValidatorPubKey)
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
					s.logger.Error("failed to get validator pubkey", fields.Slot(currentSlot), zap.Error(err))
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
				s.logger.Error("failed to submit validator registrations",
					zap.Int("registrations", len(registrations)),
					fields.Slot(currentSlot),
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

// feeRecipientProvider is used by the runner to get fee recipients for validators
type feeRecipientProvider interface {
	GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error)
}

// validatorStore is used by VRSubmitter for getting validator data
type validatorStore interface {
	SelfValidators() []*ssvtypes.SSVShare
}
