package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type CommitteeDutyGuard interface {
	StartDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error
	ValidDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error
}

type CommitteeRunner struct {
	BaseRunner          *BaseRunner
	network             specqbft.Network
	beacon              beacon.BeaconNode
	signer              ekm.BeaconSigner
	operatorSigner      ssvtypes.OperatorSigner
	valCheck            specqbft.ProposedValueCheckF
	DutyGuard           CommitteeDutyGuard
	doppelgangerHandler DoppelgangerProvider
	measurements        measurementsStore

	submittedDuties map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}
}

func NewCommitteeRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	dutyGuard CommitteeDutyGuard,
	doppelgangerHandler DoppelgangerProvider,
) (Runner, error) {
	if len(share) == 0 {
		return nil, errors.New("no shares")
	}
	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleCommittee,
			NetworkConfig:  networkConfig,
			Share:          share,
			QBFTController: qbftController,
		},
		beacon:              beacon,
		network:             network,
		signer:              signer,
		operatorSigner:      operatorSigner,
		valCheck:            valCheck,
		submittedDuties:     make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}),
		DutyGuard:           dutyGuard,
		doppelgangerHandler: doppelgangerHandler,
		measurements:        NewMeasurementsStore(),
	}, nil
}

func (cr *CommitteeRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.start_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	d, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		return traces.Errorf(span, "duty is not a CommitteeDuty: %T", duty)
	}

	span.SetAttributes(observability.DutyCountAttribute(len(d.ValidatorDuties)))

	for _, validatorDuty := range d.ValidatorDuties {
		err := cr.DutyGuard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), d.DutySlot())
		if err != nil {
			return traces.Errorf(span,
				"could not start %s duty at slot %d for validator %x: %w",
				validatorDuty.Type, d.DutySlot(), validatorDuty.PubKey, err)
		}
	}
	err := cr.BaseRunner.baseStartNewDuty(ctx, logger, cr, duty, quorum)
	if err != nil {
		return traces.Error(span, err)
	}

	cr.submittedDuties[spectypes.BNRoleAttester] = make(map[phase0.ValidatorIndex]struct{})
	cr.submittedDuties[spectypes.BNRoleSyncCommittee] = make(map[phase0.ValidatorIndex]struct{})

	span.SetStatus(codes.Ok, "")
	return nil
}

func (cr *CommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(cr)
}

func (cr *CommitteeRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &cr)
}

func (cr *CommitteeRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := cr.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode CommitteeRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (cr *CommitteeRunner) MarshalJSON() ([]byte, error) {
	type CommitteeRunnerAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       specqbft.ProposedValueCheckF
	}

	// Create object and marshal
	alias := &CommitteeRunnerAlias{
		BaseRunner:     cr.BaseRunner,
		beacon:         cr.beacon,
		network:        cr.network,
		signer:         cr.signer,
		operatorSigner: cr.operatorSigner,
		valCheck:       cr.valCheck,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

func (cr *CommitteeRunner) UnmarshalJSON(data []byte) error {
	type CommitteeRunnerAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       specqbft.ProposedValueCheckF
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeRunnerAlias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Assign fields
	cr.BaseRunner = aux.BaseRunner
	cr.beacon = aux.beacon
	cr.network = aux.network
	cr.signer = aux.signer
	cr.operatorSigner = aux.operatorSigner
	cr.valCheck = aux.valCheck
	return nil
}

func (cr *CommitteeRunner) GetBaseRunner() *BaseRunner {
	return cr.BaseRunner
}

func (cr *CommitteeRunner) GetBeaconNode() beacon.BeaconNode {
	return cr.beacon
}

func (cr *CommitteeRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return cr.valCheck
}

func (cr *CommitteeRunner) GetNetwork() specqbft.Network {
	return cr.network
}

func (cr *CommitteeRunner) GetBeaconSigner() ekm.BeaconSigner {
	return cr.signer
}

func (cr *CommitteeRunner) HasRunningDuty() bool {
	return cr.BaseRunner.hasRunningDuty()
}

func (cr *CommitteeRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre consensus phase for committee runner")
}

func (cr *CommitteeRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_committee_consensus"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(msg.SSVMessage.GetID()),
			observability.ValidatorMsgTypeAttribute(msg.SSVMessage.GetType()),
			observability.RunnerRoleAttribute(msg.SSVMessage.GetID().GetRoleType()),
		))
	defer span.End()

	span.AddEvent("checking if instance is decided")
	decided, decidedValue, err := cr.BaseRunner.baseConsensusMsgProcessing(ctx, logger, cr, msg, &spectypes.BeaconVote{})
	if err != nil {
		return traces.Errorf(span, "failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("instance is decided")
	cr.measurements.EndConsensus()
	recordConsensusDuration(ctx, cr.measurements.ConsensusTime(), spectypes.RoleCommittee)

	cr.measurements.StartPostConsensus()

	duty := cr.BaseRunner.State.StartingDuty
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	epoch := cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	version, _ := cr.BaseRunner.NetworkConfig.ForkAtEpoch(epoch)

	committeeDuty, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		return traces.Errorf(span, "duty is not a CommitteeDuty: %T", duty)
	}

	span.SetAttributes(
		observability.BeaconSlotAttribute(duty.DutySlot()),
		observability.BeaconEpochAttribute(epoch),
		observability.BeaconVersionAttribute(version),
		observability.DutyCountAttribute(len(committeeDuty.ValidatorDuties)),
	)

	span.AddEvent("signing validator duties")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg sync.WaitGroup
		// errCh is buffered because the receiver is only interested in the very 1st error sent to this channel
		// and will not read any subsequent errors. Buffering ensures that senders can send their errors and terminate without being blocked,
		// regardless of whether the receiver is still actively reading from the channel.
		errCh        = make(chan error, len(committeeDuty.ValidatorDuties))
		signaturesCh = make(chan *spectypes.PartialSignatureMessage)
		dutiesCh     = make(chan *spectypes.ValidatorDuty)

		beaconVote = decidedValue.(*spectypes.BeaconVote)
		totalAttesterDuties,
		totalSyncCommitteeDuties,
		blockedAttesterDuties atomic.Uint32
	)

	// The worker pool will throttle the parallel processing of validator duties.
	// This is mainly needed because the processing involves several outgoing HTTP calls to the Consensus Client.
	// These calls should be limited to a certain degree to reduce the pressure on the Consensus Node.
	const workerCount = 30

	go func() {
		defer close(dutiesCh)
		for _, duty := range committeeDuty.ValidatorDuties {
			if ctx.Err() != nil {
				break
			}
			dutiesCh <- duty
		}
	}()

	for range workerCount {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for validatorDuty := range dutiesCh {
				if ctx.Err() != nil {
					return
				}
				if err := cr.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
					const eventMsg = "duty is no longer valid"
					span.AddEvent(eventMsg, trace.WithAttributes(
						observability.ValidatorIndexAttribute(validatorDuty.ValidatorIndex),
						observability.ValidatorPublicKeyAttribute(validatorDuty.PubKey),
						observability.BeaconRoleAttribute(validatorDuty.Type),
					))
					logger.Warn(eventMsg, fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
					continue
				}

				switch validatorDuty.Type {
				case spectypes.BNRoleAttester:
					totalAttesterDuties.Add(1)
					isAttesterDutyBlocked, partialSigMsg, err := cr.signAttesterDuty(ctx, validatorDuty, beaconVote, version, logger)
					if err != nil {
						errCh <- fmt.Errorf("failed signing attestation data: %w", err)
						return
					}
					if isAttesterDutyBlocked {
						blockedAttesterDuties.Add(1)
						continue
					}

					signaturesCh <- partialSigMsg
				case spectypes.BNRoleSyncCommittee:
					totalSyncCommitteeDuties.Add(1)

					partialSigMsg, err := cr.BaseRunner.signBeaconObject(
						ctx,
						cr,
						validatorDuty,
						spectypes.SSZBytes(beaconVote.BlockRoot[:]),
						validatorDuty.DutySlot(),
						spectypes.DomainSyncCommittee,
					)
					if err != nil {
						errCh <- fmt.Errorf("failed signing sync committee message: %w", err)
						return
					}

					signaturesCh <- partialSigMsg
				default:
					errCh <- fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(signaturesCh)
	}()

listener:
	for {
		select {
		case err := <-errCh:
			cancel()
			return traces.Error(span, err)
		case signature, ok := <-signaturesCh:
			if !ok {
				break listener
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, signature)
		}
	}

	var (
		totalAttestations   = totalAttesterDuties.Load()
		totalSyncCommittee  = totalSyncCommitteeDuties.Load()
		blockedAttestations = blockedAttesterDuties.Load()
	)

	if totalAttestations == 0 && totalSyncCommittee == 0 {
		cr.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDutiesToExecute.Error())
		return ErrNoValidDutiesToExecute
	}

	// Avoid sending an empty message if all attester duties were blocked due to Doppelganger protection
	// and no sync committee duties exist.
	//
	// We do not mark the state as finished here because post-consensus messages must still be processed,
	// allowing validators to be marked as safe once sufficient consensus is reached.
	if totalAttestations == blockedAttestations && totalSyncCommittee == 0 {
		const eventMsg = "Skipping message broadcast: all attester duties blocked by Doppelganger protection, no sync committee duties."
		span.AddEvent(eventMsg)
		logger.Debug(eventMsg,
			zap.Uint32("attester_duties", totalAttestations),
			zap.Uint32("blocked_attesters", blockedAttestations))

		span.SetStatus(codes.Ok, "")
		return nil
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID: spectypes.NewMsgID(
			cr.BaseRunner.NetworkConfig.DomainType,
			cr.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:],
			cr.BaseRunner.RunnerRoleType,
		),
	}
	ssvMsg.Data, err = postConsensusMsg.Encode()
	if err != nil {
		return traces.Errorf(span, "failed to encode post consensus signature msg: %w", err)
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := cr.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{cr.BaseRunner.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := cr.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial post consensus sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (cr *CommitteeRunner) signAttesterDuty(
	ctx context.Context,
	validatorDuty *spectypes.ValidatorDuty,
	beaconVote *spectypes.BeaconVote,
	version spec.DataVersion,
	logger *zap.Logger) (isBlocked bool, partialSig *spectypes.PartialSignatureMessage, err error) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.sign_attester_duty"),
		trace.WithAttributes(
			observability.ValidatorIndexAttribute(validatorDuty.ValidatorIndex),
			observability.ValidatorPublicKeyAttribute(validatorDuty.PubKey),
			observability.BeaconRoleAttribute(validatorDuty.Type),
		))
	defer span.End()

	span.AddEvent("doppelganger: checking if signing is allowed")
	// Doppelganger protection applies only to attester duties since they are slashable.
	// Sync committee duties are not slashable, so they are always allowed.
	if !cr.doppelgangerHandler.CanSign(validatorDuty.ValidatorIndex) {
		const eventMsg = "signing not permitted due to Doppelganger protection"
		span.AddEvent(eventMsg)
		logger.Warn(eventMsg, fields.ValidatorIndex(validatorDuty.ValidatorIndex))

		span.SetStatus(codes.Ok, "")
		return true, nil, nil
	}

	attestationData := constructAttestationData(beaconVote, validatorDuty, version)

	span.AddEvent("signing beacon object")
	partialMsg, err := cr.BaseRunner.signBeaconObject(
		ctx,
		cr,
		validatorDuty,
		attestationData,
		validatorDuty.DutySlot(),
		spectypes.DomainAttester,
	)
	if err != nil {
		return false, partialMsg, traces.Errorf(span, "failed signing attestation data: %w", err)
	}

	attDataRoot, err := attestationData.HashTreeRoot()
	if err != nil {
		return false, partialMsg, traces.Errorf(span, "failed to hash attestation data: %w", err)
	}

	const eventMsg = "signed attestation data"
	span.AddEvent(eventMsg, trace.WithAttributes(observability.BeaconBlockRootAttribute(attDataRoot)))
	logger.Debug(eventMsg,
		zap.Uint64("validator_index", uint64(validatorDuty.ValidatorIndex)),
		zap.String("pub_key", hex.EncodeToString(validatorDuty.PubKey[:])),
		zap.Any("attestation_data", attestationData),
		zap.String("attestation_data_root", hex.EncodeToString(attDataRoot[:])),
		zap.String("signing_root", hex.EncodeToString(partialMsg.SigningRoot[:])),
		zap.String("signature", hex.EncodeToString(partialMsg.PartialSignature[:])),
	)

	span.SetStatus(codes.Ok, "")

	return false, partialMsg, nil
}

// TODO finish edge case where some roots may be missing
func (cr *CommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_committee_post_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
			attribute.Int("ssv.validator.partial_signature_msg.count", len(signedMsg.Messages)),
		))
	defer span.End()

	span.AddEvent("base post consensus message processing")
	hasQuorum, roots, err := cr.BaseRunner.basePostConsensusMsgProcessing(ctx, cr, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing post consensus message: %w", err)
	}

	indices := make([]uint64, len(signedMsg.Messages))
	for i, msg := range signedMsg.Messages {
		indices[i] = uint64(msg.ValidatorIndex)
	}
	logger = logger.With(fields.ConsensusTime(cr.measurements.ConsensusTime()))

	const eventMsg = "🧩 got partial signatures"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		zap.Bool("quorum", hasQuorum),
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Int("roots", len(roots)),
		zap.Uint64s("validators", indices))

	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("getting attestations, sync committees and root beacon objects")
	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := cr.expectedPostConsensusRootsAndBeaconObjects(ctx, logger)
	if err != nil {
		return traces.Errorf(span, "could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		cr.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDutiesToExecute.Error())
		return ErrNoValidDutiesToExecute
	}

	attestationsToSubmit := make(map[phase0.ValidatorIndex]*spec.VersionedAttestation)
	syncCommitteeMessagesToSubmit := make(map[phase0.ValidatorIndex]*altair.SyncCommitteeMessage)

	// Get unique roots to avoid repetition
	deduplicatedRoots := make(map[[32]byte]struct{})
	for _, root := range roots {
		deduplicatedRoots[root] = struct{}{}
	}

	var executionErr error

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(deduplicatedRoots)))
	// For each root that got at least one quorum, find the duties associated to it and try to submit
	for root := range deduplicatedRoots {
		// Get validators related to the given root
		role, validators, found := findValidators(root, attestationMap, committeeMap)
		if !found {
			// Edge case: since operators may have divergent sets of validators,
			// it's possible that an operator doesn't have the validator associated to a root.
			// In this case, we simply continue.
			continue
		}

		const eventMsg = "found validators for root"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconRoleAttribute(role),
			observability.BeaconBlockRootAttribute(root),
			observability.ValidatorCountAttribute(len(validators)),
		))
		logger.Debug(eventMsg,
			fields.BeaconRole(role),
			zap.String("root", hex.EncodeToString(root[:])),
			zap.Any("validators", validators),
		)

		type signatureResult struct {
			signature      phase0.BLSSignature
			validatorIndex phase0.ValidatorIndex
		}
		var (
			wg          sync.WaitGroup
			errCh       = make(chan error, len(validators))
			signatureCh = make(chan signatureResult, len(validators))
		)

		span.AddEvent("constructing sync-committee and attestations signature messages", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		for _, validator := range validators {
			// Skip if no quorum - We know that a root has quorum but not necessarily for the validator
			if !cr.BaseRunner.State.PostConsensusContainer.HasQuorum(validator, root) {
				continue
			}
			// Skip if already submitted
			if cr.HasSubmitted(role, validator) {
				continue
			}

			wg.Add(1)
			go func(validatorIndex phase0.ValidatorIndex, root [32]byte, roots map[[32]byte]struct{}) {
				defer wg.Done()

				share := cr.BaseRunner.Share[validatorIndex]

				pubKey := share.ValidatorPubKey
				vlogger := logger.With(zap.Uint64("validator_index", uint64(validatorIndex)), zap.String("pubkey", hex.EncodeToString(pubKey[:])))

				sig, err := cr.BaseRunner.State.ReconstructBeaconSig(cr.BaseRunner.State.PostConsensusContainer, root, pubKey[:], validatorIndex)
				// If the reconstructed signature verification failed, fall back to verifying each partial signature
				if err != nil {
					for root := range roots {
						cr.BaseRunner.FallBackAndVerifyEachSignature(cr.BaseRunner.State.PostConsensusContainer, root, share.Committee, validatorIndex)
					}
					const eventMsg = "got post-consensus quorum but it has invalid signatures"
					span.AddEvent(eventMsg)
					vlogger.Error(eventMsg, zap.Error(err))

					errCh <- fmt.Errorf("%s: %w", eventMsg, err)
					return
				}

				vlogger.Debug("🧩 reconstructed partial signature")

				signatureCh <- signatureResult{
					validatorIndex: validatorIndex,
					signature:      (phase0.BLSSignature)(sig),
				}
			}(validator, root, deduplicatedRoots)
		}

		go func() {
			wg.Wait()
			close(signatureCh)
		}()

	listener:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				executionErr = err
			case signatureResult, ok := <-signatureCh:
				if !ok {
					break listener
				}

				validatorObjects, exists := beaconObjects[signatureResult.validatorIndex]
				if !exists {
					executionErr = fmt.Errorf("could not find beacon object for validator index: %d", signatureResult.validatorIndex)
					continue
				}
				sszObject, exists := validatorObjects[root]
				if !exists {
					executionErr = fmt.Errorf("could not find ssz object for root: %s", root)
					continue
				}

				// Store objects for multiple submission
				if role == spectypes.BNRoleSyncCommittee {
					syncMsg := sszObject.(*altair.SyncCommitteeMessage)
					syncMsg.Signature = signatureResult.signature

					syncCommitteeMessagesToSubmit[signatureResult.validatorIndex] = syncMsg
				} else if role == spectypes.BNRoleAttester {
					// Only mark as safe if this is an attester role
					// We want to mark the validator as safe as soon as possible to minimize unnecessary delays in enabling signing.
					// The doppelganger check is not performed for sync committee duties, so we rely on attester duties for safety confirmation.
					cr.doppelgangerHandler.ReportQuorum(signatureResult.validatorIndex)

					att := sszObject.(*spec.VersionedAttestation)
					att, err = specssv.VersionedAttestationWithSignature(att, signatureResult.signature)
					if err != nil {
						executionErr = fmt.Errorf("could not insert signature in versioned attestation")
						continue
					}

					attestationsToSubmit[signatureResult.validatorIndex] = att
				}
			}
		}

		logger.Debug("🧩 reconstructed partial signatures for root",
			zap.Uint64s("signers", getPostConsensusCommitteeSigners(cr.BaseRunner.State, root)),
			fields.BlockRoot(root),
		)
	}

	cr.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, cr.measurements.PostConsensusTime(), spectypes.RoleCommittee)

	logger = logger.With(fields.PostConsensusTime(cr.measurements.PostConsensusTime()))

	attestations := make([]*spec.VersionedAttestation, 0, len(attestationsToSubmit))
	for _, att := range attestationsToSubmit {
		if att != nil && att.ValidatorIndex != nil {
			attestations = append(attestations, att)
		}
	}

	cr.measurements.EndDutyFlow()

	if len(attestations) > 0 {
		span.AddEvent("submitting attestations")
		submissionStart := time.Now()

		// Submit multiple attestations
		if err := cr.beacon.SubmitAttestations(ctx, attestations); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleAttester)

			const errMsg = "could not submit attestations"
			logger.Error(errMsg, zap.Error(err))
			return traces.Errorf(span, "%s: %w", errMsg, err)
		}

		recordDutyDuration(ctx, cr.measurements.TotalDutyTime(), spectypes.BNRoleAttester, cr.BaseRunner.State.RunningInstance.State.Round)

		attestationsCount := len(attestations)
		if attestationsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(
				ctx,
				uint32(attestationsCount),
				cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleAttester,
			)
		}

		attData, err := attestations[0].Data()
		if err != nil {
			return traces.Errorf(span, "could not get attestation data: %w", err)
		}
		const eventMsg = "✅ successfully submitted attestations"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconBlockRootAttribute(attData.BeaconBlockRoot),
			observability.DutyRoundAttribute(cr.BaseRunner.State.RunningInstance.State.Round),
			observability.ValidatorCountAttribute(attestationsCount),
		))

		logger.Info(eventMsg,
			fields.Epoch(cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot())),
			fields.QBFTHeight(cr.BaseRunner.QBFTController.Height),
			fields.QBFTRound(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(attData.BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(cr.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(cr.measurements.TotalDutyTime()),
			fields.Count(attestationsCount),
		)

		// Record successful submissions
		for validator := range attestationsToSubmit {
			cr.RecordSubmission(spectypes.BNRoleAttester, validator)
		}
	}

	// Submit multiple sync committee
	syncCommitteeMessages := make([]*altair.SyncCommitteeMessage, 0, len(syncCommitteeMessagesToSubmit))
	for _, syncMsg := range syncCommitteeMessagesToSubmit {
		syncCommitteeMessages = append(syncCommitteeMessages, syncMsg)
	}

	if len(syncCommitteeMessages) > 0 {
		span.AddEvent("submitting sync committee")
		submissionStart := time.Now()
		if err := cr.beacon.SubmitSyncMessages(ctx, syncCommitteeMessages); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleSyncCommittee)

			const errMsg = "could not submit sync committee messages"
			logger.Error(errMsg, zap.Error(err))
			return traces.Errorf(span, "%s: %w", errMsg, err)
		}

		recordDutyDuration(ctx, cr.measurements.TotalDutyTime(), spectypes.BNRoleSyncCommittee, cr.BaseRunner.State.RunningInstance.State.Round)

		syncMsgsCount := len(syncCommitteeMessages)
		if syncMsgsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(
				ctx,
				uint32(syncMsgsCount),
				cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleSyncCommittee,
			)
		}
		const eventMsg = "✅ successfully submitted sync committee"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconSlotAttribute(cr.BaseRunner.State.StartingDuty.DutySlot()),
			observability.DutyRoundAttribute(cr.BaseRunner.State.RunningInstance.State.Round),
			observability.BeaconBlockRootAttribute(syncCommitteeMessages[0].BeaconBlockRoot),
			observability.ValidatorCountAttribute(len(syncCommitteeMessages)),
			attribute.Float64("ssv.validator.duty.submission_time", time.Since(submissionStart).Seconds()),
			attribute.Float64("ssv.validator.duty.consensus_time_total", time.Since(cr.measurements.consensusStart).Seconds()),
		))
		logger.Info(eventMsg,
			fields.QBFTHeight(cr.BaseRunner.QBFTController.Height),
			fields.QBFTRound(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(syncCommitteeMessages[0].BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(cr.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(cr.measurements.TotalDutyTime()),
			fields.Count(syncMsgsCount),
		)

		// Record successful submissions
		for validator := range syncCommitteeMessagesToSubmit {
			cr.RecordSubmission(spectypes.BNRoleSyncCommittee, validator)
		}
	}

	if executionErr != nil {
		span.SetStatus(codes.Error, executionErr.Error())
		return executionErr
	}

	// Check if duty has terminated (runner has submitted for all duties)
	if cr.HasSubmittedAllValidatorDuties(attestationMap, committeeMap) {
		cr.BaseRunner.State.Finished = true
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// HasSubmittedAllValidatorDuties -- Returns true if the runner has done submissions for all validators for the given slot
func (cr *CommitteeRunner) HasSubmittedAllValidatorDuties(attestationMap map[phase0.ValidatorIndex][32]byte, syncCommitteeMap map[phase0.ValidatorIndex][32]byte) bool {
	// Expected total
	expectedTotalSubmissions := len(attestationMap) + len(syncCommitteeMap)

	totalSubmissions := 0

	// Add submitted attestation duties
	for valIdx := range attestationMap {
		if cr.HasSubmitted(spectypes.BNRoleAttester, valIdx) {
			totalSubmissions++
		}
	}
	// Add submitted sync committee duties
	for valIdx := range syncCommitteeMap {
		if cr.HasSubmitted(spectypes.BNRoleSyncCommittee, valIdx) {
			totalSubmissions++
		}
	}
	return totalSubmissions >= expectedTotalSubmissions
}

// RecordSubmission -- Records a submission for the (role, validator index, slot) tuple
func (cr *CommitteeRunner) RecordSubmission(role spectypes.BeaconRole, valIdx phase0.ValidatorIndex) {
	if _, ok := cr.submittedDuties[role]; !ok {
		cr.submittedDuties[role] = make(map[phase0.ValidatorIndex]struct{})
	}
	cr.submittedDuties[role][valIdx] = struct{}{}
}

// HasSubmitted -- Returns true if there is a record of submission for the (role, validator index, slot) tuple
func (cr *CommitteeRunner) HasSubmitted(role spectypes.BeaconRole, valIdx phase0.ValidatorIndex) bool {
	if _, ok := cr.submittedDuties[role]; !ok {
		return false
	}
	_, ok := cr.submittedDuties[role][valIdx]
	return ok
}

func findValidators(
	expectedRoot [32]byte,
	attestationMap map[phase0.ValidatorIndex][32]byte,
	committeeMap map[phase0.ValidatorIndex][32]byte) (spectypes.BeaconRole, []phase0.ValidatorIndex, bool) {
	var validators []phase0.ValidatorIndex

	// look for the expectedRoot in attestationMap
	for validator, root := range attestationMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleAttester, validators, true
	}
	// look for the expectedRoot in committeeMap
	for validator, root := range committeeMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleSyncCommittee, validators, true
	}
	return spectypes.BNRoleUnknown, nil, false
}

// Unneeded since no preconsensus phase
func (cr *CommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no pre consensus root for committee runner")
}

// This function signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (cr *CommitteeRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("unexpected expectedPostConsensusRootsAndDomain func call")
}

func (cr *CommitteeRunner) expectedPostConsensusRootsAndBeaconObjects(ctx context.Context, logger *zap.Logger) (
	attestationMap map[phase0.ValidatorIndex][32]byte,
	syncCommitteeMap map[phase0.ValidatorIndex][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]interface{}, err error,
) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]interface{})
	duty := cr.BaseRunner.State.StartingDuty
	// TODO DecidedValue should be interface??
	beaconVoteData := cr.BaseRunner.State.DecidedValue
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(beaconVoteData); err != nil {
		return nil, nil, nil, fmt.Errorf("could not decode beacon vote: %w", err)
	}

	slot := duty.DutySlot()
	epoch := cr.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(slot)

	dataVersion, _ := cr.GetBaseRunner().NetworkConfig.ForkAtEpoch(epoch)

	for _, validatorDuty := range duty.(*spectypes.CommitteeDuty).ValidatorDuties {
		if validatorDuty == nil {
			continue
		}
		if err := cr.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
			logger.Warn("duty is no longer valid", fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
			continue
		}
		logger := logger.With(fields.Validator(validatorDuty.PubKey[:]))
		slot := validatorDuty.DutySlot()
		epoch := cr.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(slot)
		switch validatorDuty.Type {
		case spectypes.BNRoleAttester:
			// Attestation object
			attestationData := constructAttestationData(beaconVote, validatorDuty, dataVersion)
			attestationResponse, err := specssv.ConstructVersionedAttestationWithoutSignature(attestationData, dataVersion, validatorDuty)
			if err != nil {
				logger.Debug("failed to construct attestation", zap.Error(err))
				continue
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainAttester)
			if err != nil {
				logger.Debug("failed to get attester domain", zap.Error(err))
				continue
			}

			root, err := spectypes.ComputeETHSigningRoot(attestationData, domain)
			if err != nil {
				logger.Debug("failed to compute attester root", zap.Error(err))
				continue
			}

			// Add to map
			attestationMap[validatorDuty.ValidatorIndex] = root
			if _, ok := beaconObjects[validatorDuty.ValidatorIndex]; !ok {
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]interface{})
			}
			beaconObjects[validatorDuty.ValidatorIndex][root] = attestationResponse
		case spectypes.BNRoleSyncCommittee:
			// Sync committee beacon object
			syncMsg := &altair.SyncCommitteeMessage{
				Slot:            slot,
				BeaconBlockRoot: beaconVote.BlockRoot,
				ValidatorIndex:  validatorDuty.ValidatorIndex,
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainSyncCommittee)
			if err != nil {
				logger.Debug("failed to get sync committee domain", zap.Error(err))
				continue
			}
			// Eth root
			blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
			root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
			if err != nil {
				logger.Debug("failed to compute sync committee root", zap.Error(err))
				continue
			}

			// Set root and beacon object
			syncCommitteeMap[validatorDuty.ValidatorIndex] = root
			if _, ok := beaconObjects[validatorDuty.ValidatorIndex]; !ok {
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]interface{})
			}
			beaconObjects[validatorDuty.ValidatorIndex][root] = syncMsg
		default:
			return nil, nil, nil, fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
		}
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

func (cr *CommitteeRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	cr.measurements.StartDutyFlow()

	start := time.Now()
	slot := duty.DutySlot()

	attData, _, err := cr.GetBeaconNode().GetAttestationData(ctx, slot)
	if err != nil {
		return traces.Errorf(span, "failed to get attestation data: %w", err)
	}

	logger = logger.With(zap.Duration("attestation_data_time", time.Since(start)))

	cr.measurements.StartConsensus()

	vote := &spectypes.BeaconVote{
		BlockRoot: attData.BeaconBlockRoot,
		Source:    attData.Source,
		Target:    attData.Target,
	}

	if err := cr.BaseRunner.decide(ctx, logger, cr, duty.DutySlot(), vote); err != nil {
		return traces.Errorf(span, "failed to start new duty runner instance: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (cr *CommitteeRunner) GetSigner() ekm.BeaconSigner {
	return cr.signer
}

func (cr *CommitteeRunner) GetDoppelgangerHandler() DoppelgangerProvider {
	return cr.doppelgangerHandler
}

func (cr *CommitteeRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return cr.operatorSigner
}

func constructAttestationData(vote *spectypes.BeaconVote, duty *spectypes.ValidatorDuty, version spec.DataVersion) *phase0.AttestationData {
	attData := &phase0.AttestationData{
		Slot:            duty.Slot,
		Index:           duty.CommitteeIndex,
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
	if version >= spec.DataVersionElectra {
		attData.Index = 0 // EIP-7549: Index should be set to 0
	}
	return attData
}
