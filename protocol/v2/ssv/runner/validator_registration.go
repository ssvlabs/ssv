package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cespare/xxhash/v2"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	DefaultGasLimit = uint64(36_000_000)
)

type ValidatorRegistrationRunner struct {
	*BaseRunner

	beacon                         beacon.BeaconNode
	network                        protocolp2p.Network
	signer                         ekm.BeaconSigner
	operatorSigner                 ssvtypes.OperatorSigner
	validatorRegistrationSubmitter ValidatorRegistrationSubmitter
	feeRecipientProvider           feeRecipientProvider

	gasLimit uint64
}

// ValidatorRegistrationRunnerOptions bundles all dependencies required by NewValidatorRegistrationRunner.
type ValidatorRegistrationRunnerOptions struct {
	BaseRunnerOptions

	ValidatorRegistrationSubmitter ValidatorRegistrationSubmitter
	FeeRecipientProvider           feeRecipientProvider
	GasLimit                       uint64
}

func NewValidatorRegistrationRunner(opts ValidatorRegistrationRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, fmt.Errorf("must have one share")
	}

	return &ValidatorRegistrationRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleValidatorRegistration,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},

		beacon:                         opts.Beacon,
		network:                        opts.Network,
		signer:                         opts.Signer,
		operatorSigner:                 opts.OperatorSigner,
		validatorRegistrationSubmitter: opts.ValidatorRegistrationSubmitter,
		feeRecipientProvider:           opts.FeeRecipientProvider,

		gasLimit: opts.GasLimit,
	}, nil
}

func (r *ValidatorRegistrationRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *ValidatorRegistrationRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing validator registration message: %w", err)
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

	validatorDuty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	registration, err := r.buildValidatorRegistration(validatorDuty.DutySlot())
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
		zap.String("signature", hex.EncodeToString(specSig[:])),
	)

	r.markDutySucceeded()
	const dutyFinishedEvent = "✔️successfully finished duty processing"
	logger.Info(dutyFinishedEvent)
	span.AddEvent(dutyFinishedEvent)

	return nil
}

func (r *ValidatorRegistrationRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return spectypes.NewError(spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode, "no consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return spectypes.NewError(spectypes.ValidatorRegistrationNoPostConsensusPhaseErrorCode, "no post consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("current duty slot: %w", err)
	}
	vr, err := r.buildValidatorRegistration(currentDutySlot)
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("could not calculate validator registration: %w", err)
	}
	return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ValidatorRegistrationRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, fmt.Errorf("no post consensus roots for validator registration")
}

func (r *ValidatorRegistrationRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	vr, err := r.buildValidatorRegistration(validatorDuty.DutySlot())
	if err != nil {
		return fmt.Errorf("could not calculate validator registration: %w", err)
	}

	// sign partial randao
	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		validatorDuty,
		vr,
		validatorDuty.DutySlot(),
		spectypes.DomainApplicationBuilder,
	)
	if err != nil {
		return fmt.Errorf("could not sign validator registration: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ValidatorRegistrationPartialSig,
		Slot:     validatorDuty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	logger.Debug("signing and broadcasting validator registration partial sig", zap.Any("validator_registration", vr))

	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey, msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast validator registration partial sig: %w", err)
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
		gasLimit = DefaultGasLimit
	}

	epoch := r.NetworkConfig.EstimatedEpochAtSlot(slot)
	return &v1.ValidatorRegistration{
		FeeRecipient: feeRecipient,
		GasLimit:     gasLimit,
		Timestamp:    r.NetworkConfig.EpochStartTime(epoch),
		Pubkey:       phase0.BLSPubKey(validatorPubKey),
	}, nil
}

func (r *ValidatorRegistrationRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *ValidatorRegistrationRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ValidatorRegistrationRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *ValidatorRegistrationRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *ValidatorRegistrationRunner) MarshalJSON() ([]byte, error) {
	type validatorRegistrationRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}

	return json.Marshal(&validatorRegistrationRunnerJSON{
		BaseRunner: r.BaseRunner,
	})
}

func (r *ValidatorRegistrationRunner) UnmarshalJSON(data []byte) error {
	type validatorRegistrationRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}

	aux := &validatorRegistrationRunnerJSON{}
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
func (r *ValidatorRegistrationRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ValidatorRegistrationRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
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
	logger *zap.Logger,
	beaconConfig *networkconfig.Beacon,
	beacon beacon.BeaconNode,
	validatorStore validatorStore,
) *VRSubmitter {
	return &VRSubmitter{
		logger:         logger,
		beaconConfig:   beaconConfig,
		beacon:         beacon,
		validatorStore: validatorStore,
		registrations:  map[phase0.BLSPubKey]*validatorRegistration{},
	}
}

// Start runs the registration-submission loop until ctx is canceled.
func (s *VRSubmitter) Start(ctx context.Context) {
	slotTicker := slotticker.New(s.logger, slotticker.Config{
		SlotDuration: s.beaconConfig.SlotDuration,
		GenesisTime:  s.beaconConfig.GenesisTime,
	})
	s.start(ctx, slotTicker)
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
			// Validator registration is deprecated at the Gloas fork; stop submitting once it's active.
			if config.IsGloas(currentEpoch) {
				continue
			}
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
