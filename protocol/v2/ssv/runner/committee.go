package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

var (
	ErrNoValidDuties = errors.New("no valid duties")
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
	networkConfig networkconfig.Network,
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
		return fmt.Errorf("duty is not a CommitteeDuty: %T", duty)
	}

	span.SetAttributes(observability.DutyCountAttribute(len(d.ValidatorDuties)))

	for _, validatorDuty := range d.ValidatorDuties {
		err := cr.DutyGuard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), d.DutySlot())
		if err != nil {
			return observability.Errorf(span,
				"could not start %s duty at slot %d for validator %x: %w",
				validatorDuty.Type, d.DutySlot(), validatorDuty.PubKey, err)
		}
	}
	err := cr.BaseRunner.baseStartNewDuty(ctx, logger, cr, duty, quorum)
	if err != nil {
		return observability.Error(span, err)
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
		return [32]byte{}, errors.Wrap(err, "could not encode CommitteeRunner")
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

	decided, decidedValue, err := cr.BaseRunner.baseConsensusMsgProcessing(ctx, logger, cr, msg, &spectypes.BeaconVote{})
	if err != nil {
		return observability.Errorf(span, "failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	cr.measurements.EndConsensus()
	recordConsensusDuration(ctx, cr.measurements.ConsensusTime(), spectypes.RoleCommittee)

	cr.measurements.StartPostConsensus()
	// decided means consensus is done

	duty := cr.BaseRunner.State.StartingDuty
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	beaconVote := decidedValue.(*spectypes.BeaconVote)
	totalAttesterDuties := 0
	totalSyncCommitteeDuties := 0
	blockedAttesterDuties := 0

	epoch := cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	version, _ := cr.BaseRunner.NetworkConfig.ForkAtEpoch(epoch)

	span.SetAttributes(
		observability.BeaconSlotAttribute(duty.DutySlot()),
		observability.BeaconEpochAttribute(epoch),
		observability.BeaconVersionAttribute(version),
	)

	for _, validatorDuty := range duty.(*spectypes.CommitteeDuty).ValidatorDuties {
		attr := trace.WithAttributes(
			observability.ValidatorIndexAttribute(validatorDuty.ValidatorIndex),
			observability.ValidatorPublicKeyAttribute(validatorDuty.PubKey),
			observability.BeaconRoleAttribute(validatorDuty.Type),
		)

		span.AddEvent("validating duty", attr)
		if err := cr.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
			const eventMsg = "duty is no longer valid"
			span.AddEvent(eventMsg, attr)
			logger.Warn(eventMsg, fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
			continue
		}

		switch validatorDuty.Type {
		case spectypes.BNRoleAttester:
			totalAttesterDuties++

			// Doppelganger protection applies only to attester duties since they are slashable.
			// Sync committee duties are not slashable, so they are always allowed.
			if !cr.doppelgangerHandler.CanSign(validatorDuty.ValidatorIndex) {
				const eventMsg = "Signing not permitted due to Doppelganger protection"
				span.AddEvent(eventMsg, attr)
				logger.Warn(eventMsg, fields.ValidatorIndex(validatorDuty.ValidatorIndex))
				blockedAttesterDuties++
				continue
			}

			span.AddEvent("constructing attestation data", attr)
			attestationData := constructAttestationData(beaconVote, validatorDuty, version)

			span.AddEvent("signing attestation data", attr)
			partialMsg, err := cr.BaseRunner.signBeaconObject(
				ctx,
				cr,
				validatorDuty,
				attestationData,
				validatorDuty.DutySlot(),
				spectypes.DomainAttester,
			)
			if err != nil {
				return observability.Errorf(span, "failed signing attestation data: %w", err)
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

			// TODO: revert log
			attDataRoot, err := attestationData.HashTreeRoot()
			if err != nil {
				return observability.Errorf(span, "failed to hash attestation data: %w", err)
			}
			const eventMsg = "signed attestation data"
			logger.Debug(eventMsg,
				zap.Uint64("validator_index", uint64(validatorDuty.ValidatorIndex)),
				zap.String("pub_key", hex.EncodeToString(validatorDuty.PubKey[:])),
				zap.Any("attestation_data", attestationData),
				zap.String("attestation_data_root", hex.EncodeToString(attDataRoot[:])),
				zap.String("signing_root", hex.EncodeToString(partialMsg.SigningRoot[:])),
				zap.String("signature", hex.EncodeToString(partialMsg.PartialSignature[:])),
			)
			span.AddEvent(eventMsg, attr)

		case spectypes.BNRoleSyncCommittee:
			totalSyncCommitteeDuties++

			blockRoot := beaconVote.BlockRoot
			partialMsg, err := cr.BaseRunner.signBeaconObject(
				ctx,
				cr,
				validatorDuty,
				spectypes.SSZBytes(blockRoot[:]),
				validatorDuty.DutySlot(),
				spectypes.DomainSyncCommittee,
			)
			if err != nil {
				return observability.Errorf(span, "failed signing sync committee message: %w", err)
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

		default:
			return fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
		}
	}

	if totalAttesterDuties == 0 && totalSyncCommitteeDuties == 0 {
		cr.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDuties.Error())
		return ErrNoValidDuties
	}

	// Avoid sending an empty message if all attester duties were blocked due to Doppelganger protection
	// and no sync committee duties exist.
	//
	// We do not mark the state as finished here because post-consensus messages must still be processed,
	// allowing validators to be marked as safe once sufficient consensus is reached.
	if totalAttesterDuties == blockedAttesterDuties && totalSyncCommitteeDuties == 0 {
		const eventMsg = "Skipping message broadcast: all attester duties blocked by Doppelganger protection, no sync committee duties."
		span.AddEvent(eventMsg)
		logger.Debug(eventMsg,
			zap.Int("attester_duties", totalAttesterDuties),
			zap.Int("blocked_attesters", blockedAttesterDuties))

		span.SetStatus(codes.Ok, "")
		return nil
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID: spectypes.NewMsgID(
			cr.BaseRunner.NetworkConfig.GetDomainType(),
			cr.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:],
			cr.BaseRunner.RunnerRoleType,
		),
	}
	ssvMsg.Data, err = postConsensusMsg.Encode()
	if err != nil {
		return observability.Errorf(span, "failed to encode post consensus signature msg: %w", err)
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := cr.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return observability.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{cr.BaseRunner.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := cr.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return observability.Errorf(span, "can't broadcast partial post consensus sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// TODO finish edge case where some roots may be missing
func (cr *CommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_committee_post_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := cr.BaseRunner.basePostConsensusMsgProcessing(ctx, cr, signedMsg)
	if err != nil {
		return observability.Errorf(span, "failed processing post consensus message: %w", err)
	}

	span.SetAttributes(
		observability.ValidatorHasQuorumAttribute(hasQuorum),
		observability.BeaconBlockRootCountAttribute(len(roots)),
	)
	logger = logger.With(fields.Slot(signedMsg.Slot))

	// TODO: (Alan) revert?
	indices := make([]uint64, len(signedMsg.Messages))
	signers := make([]uint64, len(signedMsg.Messages))
	for i, msg := range signedMsg.Messages {
		signers[i] = msg.Signer
		indices[i] = uint64(msg.ValidatorIndex)
	}
	logger = logger.With(fields.ConsensusTime(cr.measurements.ConsensusTime()))

	const eventMsg = "🧩 got partial signatures"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		zap.Bool("quorum", hasQuorum),
		fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Int("roots", len(roots)),
		zap.Uint64s("validators", indices))

	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := cr.expectedPostConsensusRootsAndBeaconObjects(ctx, logger)
	if err != nil {
		return observability.Errorf(span, "could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		cr.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDuties.Error())
		return ErrNoValidDuties
	}

	var anyErr error
	attestationsToSubmit := make(map[phase0.ValidatorIndex]*spec.VersionedAttestation)
	syncCommitteeMessagesToSubmit := make(map[phase0.ValidatorIndex]*altair.SyncCommitteeMessage)

	// Get unique roots to avoid repetition
	rootSet := make(map[[32]byte]struct{})
	for _, root := range roots {
		rootSet[root] = struct{}{}
	}
	// For each root that got at least one quorum, find the duties associated to it and try to submit
	for root := range rootSet {
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
		))
		logger.Debug(eventMsg,
			fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
			zap.String("role", role.String()),
			zap.String("root", hex.EncodeToString(root[:])),
			zap.Any("validators", validators),
		)

		for _, validator := range validators {
			// Skip if no quorum - We know that a root has quorum but not necessarily for the validator
			if !cr.BaseRunner.State.PostConsensusContainer.HasQuorum(validator, root) {
				continue
			}
			// Skip if already submitted
			if cr.HasSubmitted(role, validator) {
				continue
			}

			// Reconstruct signature
			share := cr.BaseRunner.Share[validator]
			pubKey := share.ValidatorPubKey
			vlogger := logger.With(zap.Uint64("validator_index", uint64(validator)), zap.String("pubkey", hex.EncodeToString(pubKey[:])))

			sig, err := cr.BaseRunner.State.ReconstructBeaconSig(cr.BaseRunner.State.PostConsensusContainer, root,
				pubKey[:], validator)
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			// TODO should we return an error here? maybe other sigs are fine?
			if err != nil {
				for root := range rootSet {
					cr.BaseRunner.FallBackAndVerifyEachSignature(cr.BaseRunner.State.PostConsensusContainer, root,
						share.Committee, validator)
				}
				const eventMsg = "got post-consensus quorum but it has invalid signatures"
				span.AddEvent(eventMsg)
				vlogger.Error(eventMsg,
					fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
					zap.Error(err),
				)

				anyErr = errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
				continue
			}
			specSig := phase0.BLSSignature{}
			copy(specSig[:], sig)

			const eventMsg = "🧩 reconstructed partial signatures committee"
			span.AddEvent(eventMsg)
			vlogger.Debug(eventMsg, zap.Uint64s("signers", getPostConsensusCommitteeSigners(cr.BaseRunner.State, root)))
			// Get the beacon object related to root
			validatorObjs, exists := beaconObjects[validator]
			if !exists {
				anyErr = errors.Wrap(err, "could not find beacon object for validator")
				continue
			}
			sszObject, exists := validatorObjs[root]
			if !exists {
				anyErr = errors.Wrap(err, "could not find beacon object for validator")
				continue
			}

			// Store objects for multiple submission
			if role == spectypes.BNRoleSyncCommittee {
				syncMsg := sszObject.(*altair.SyncCommitteeMessage)
				// Insert signature
				syncMsg.Signature = specSig

				syncCommitteeMessagesToSubmit[validator] = syncMsg

			} else if role == spectypes.BNRoleAttester {
				// Only mark as safe if this is an attester role
				// We want to mark the validator as safe as soon as possible to minimize unnecessary delays in enabling signing.
				// The doppelganger check is not performed for sync committee duties, so we rely on attester duties for safety confirmation.
				cr.doppelgangerHandler.ReportQuorum(validator)

				att := sszObject.(*spec.VersionedAttestation)
				// Insert signature
				att, err = specssv.VersionedAttestationWithSignature(att, specSig)
				if err != nil {
					anyErr = errors.Wrap(err, "could not insert signature in versioned attestation")
					continue
				}

				attestationsToSubmit[validator] = att
			}
		}
	}

	cr.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, cr.measurements.PostConsensusTime(), spectypes.RoleCommittee)

	logger = logger.With(fields.PostConsensusTime(cr.measurements.PostConsensusTime()))

	// Submit multiple attestations
	attestations := make([]*spec.VersionedAttestation, 0, len(attestationsToSubmit))
	for _, att := range attestationsToSubmit {
		if att != nil && att.ValidatorIndex != nil {
			span.AddEvent("adding attestation to submit to beacon", trace.WithAttributes(
				observability.ValidatorIndexAttribute(*att.ValidatorIndex),
			))

			attestations = append(attestations, att)
		}
	}

	cr.measurements.EndDutyFlow()

	if len(attestations) > 0 {
		submissionStart := time.Now()
		span.AddEvent("submitting attestations")
		if err := cr.beacon.SubmitAttestations(ctx, attestations); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleAttester)

			const errMsg = "could not submit attestations"
			logger.Error(errMsg, zap.Error(err))
			return observability.Errorf(span, "%s: %w", errMsg, err)
		}
		recordDutyDuration(ctx, cr.measurements.DutyDurationTime(), spectypes.BNRoleAttester, cr.BaseRunner.State.RunningInstance.State.Round)
		attestationsCount := len(attestations)
		if attestationsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(ctx,
				uint32(attestationsCount),
				cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleAttester)
		}

		attData, err := attestations[0].Data()
		if err != nil {
			return observability.Errorf(span, "could not get attestation data: %w", err)
		}
		const eventMsg = "✅ successfully submitted attestations"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconBlockRootAttribute(attData.BeaconBlockRoot),
			observability.DutyRoundAttribute(cr.BaseRunner.State.RunningInstance.State.Round),
			observability.ValidatorCountAttribute(attestationsCount),
		))

		logger.Info(eventMsg,
			fields.Epoch(cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot())),
			fields.Height(cr.BaseRunner.QBFTController.Height),
			fields.Round(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(attData.BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(cr.measurements.TotalConsensusTime()),
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
		submissionStart := time.Now()
		if err := cr.beacon.SubmitSyncMessages(ctx, syncCommitteeMessages); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleSyncCommittee)

			const errMsg = "could not submit sync committee messages"
			logger.Error(errMsg, zap.Error(err))
			return observability.Errorf(span, "%s: %w", errMsg, err)
		}
		recordDutyDuration(ctx, cr.measurements.DutyDurationTime(), spectypes.BNRoleSyncCommittee, cr.BaseRunner.State.RunningInstance.State.Round)
		syncMsgsCount := len(syncCommitteeMessages)
		if syncMsgsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(ctx,
				uint32(syncMsgsCount),
				cr.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleSyncCommittee)
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
			fields.Height(cr.BaseRunner.QBFTController.Height),
			fields.Round(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(syncCommitteeMessages[0].BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(cr.measurements.TotalConsensusTime()),
			fields.Count(syncMsgsCount),
		)

		// Record successful submissions
		for validator := range syncCommitteeMessagesToSubmit {
			cr.RecordSubmission(spectypes.BNRoleSyncCommittee, validator)
		}
	}

	if anyErr != nil {
		span.SetStatus(codes.Error, anyErr.Error())
		return anyErr
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
func (cr CommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no pre consensus root for committee runner")
}

// This function signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (cr CommitteeRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("expected post consensus roots function is unused")
}

func (cr *CommitteeRunner) expectedPostConsensusRootsAndBeaconObjects(ctx context.Context, logger *zap.Logger) (
	attestationMap map[phase0.ValidatorIndex][32]byte,
	syncCommitteeMap map[phase0.ValidatorIndex][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]interface{}, error error,
) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]interface{})
	duty := cr.BaseRunner.State.StartingDuty
	// TODO DecidedValue should be interface??
	beaconVoteData := cr.BaseRunner.State.DecidedValue
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(beaconVoteData); err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not decode beacon vote")
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
		return observability.Errorf(span, "failed to get attestation data: %w", err)
	}

	logger = logger.With(
		zap.Duration("attestation_data_time", time.Since(start)),
		fields.Slot(slot),
	)

	cr.measurements.StartConsensus()

	vote := &spectypes.BeaconVote{
		BlockRoot: attData.BeaconBlockRoot,
		Source:    attData.Source,
		Target:    attData.Target,
	}

	if err := cr.BaseRunner.decide(ctx, logger, cr, duty.DutySlot(), vote); err != nil {
		return observability.Errorf(span, "failed to start new duty runner instance: %w", err)
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
