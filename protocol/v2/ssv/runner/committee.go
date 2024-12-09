package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
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
	BaseRunner     *BaseRunner
	network        specqbft.Network
	beacon         beacon.BeaconNode
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF
	DutyGuard      CommitteeDutyGuard
	measurements   measurementsStore

	submittedDuties map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}
}

func NewCommitteeRunner(
	networkConfig networkconfig.NetworkConfig,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	dutyGuard CommitteeDutyGuard,
) (Runner, error) {
	if len(share) == 0 {
		return nil, errors.New("no shares")
	}
	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleCommittee,
			DomainType:     networkConfig.DomainType,
			BeaconNetwork:  networkConfig.Beacon.GetBeaconNetwork(),
			Share:          share,
			QBFTController: qbftController,
		},
		beacon:          beacon,
		network:         network,
		signer:          signer,
		operatorSigner:  operatorSigner,
		valCheck:        valCheck,
		submittedDuties: make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}),
		DutyGuard:       dutyGuard,
		measurements:    NewMeasurementsStore(),
	}, nil
}

func (cr *CommitteeRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	ctx, span := tracer.Start(ctx,
		fmt.Sprintf("%s.runner.start_new_duty", observabilityNamespace),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			attribute.Int64("ssv.validator.quorum", int64(quorum)),
			attribute.Int64("ssv.validator.duty.slot", int64(duty.DutySlot()))))
	defer span.End()

	d, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		err := errors.New("duty is not a CommitteeDuty")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("ssv.validator.duty_count", len(d.ValidatorDuties)))

	for _, validatorDuty := range d.ValidatorDuties {
		err := cr.DutyGuard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), d.DutySlot())
		if err != nil {
			err := fmt.Errorf("could not start %s duty at slot %d for validator %x: %w",
				validatorDuty.Type, d.DutySlot(), validatorDuty.PubKey, err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	err := cr.BaseRunner.baseStartNewDuty(ctx, logger, cr, duty, quorum)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
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
		signer         spectypes.BeaconSigner
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
		signer         spectypes.BeaconSigner
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

func (cr *CommitteeRunner) GetBeaconSigner() spectypes.BeaconSigner {
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
		fmt.Sprintf("%s.runner.process_consensus", observabilityNamespace),
		trace.WithAttributes(
			attribute.String("ssv.validator.msg_id", msg.SSVMessage.MsgID.String()),
			attribute.Int64("ssv.validator.msg_type", int64(msg.SSVMessage.MsgType)),
			observability.RunnerRoleAttribute(msg.SSVMessage.GetID().GetRoleType()),
		))
	defer span.End()

	decided, decidedValue, err := cr.BaseRunner.baseConsensusMsgProcessing(ctx, logger, cr, msg, &spectypes.BeaconVote{})
	if err != nil {
		err := errors.Wrap(err, "failed processing consensus message")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
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
	validDuties := 0
	for _, duty := range duty.(*spectypes.CommitteeDuty).ValidatorDuties {
		span.SetAttributes(
			attribute.Int64("ssv.validator.index", int64(duty.ValidatorIndex)),
			attribute.String("ssv.validator.pubkey", duty.PubKey.String()),
			observability.BeaconRoleAttribute(duty.Type),
		)
		if err := cr.DutyGuard.ValidDuty(duty.Type, spectypes.ValidatorPK(duty.PubKey), duty.DutySlot()); err != nil {
			eventMsg := "duty is no longer valid"
			span.AddEvent(eventMsg)
			logger.Warn(eventMsg, fields.Validator(duty.PubKey[:]), fields.BeaconRole(duty.Type), zap.Error(err))
			continue
		}
		switch duty.Type {
		case spectypes.BNRoleAttester:
			validDuties++
			attestationData := constructAttestationData(beaconVote, duty)
			partialMsg, err := cr.BaseRunner.signBeaconObject(cr, duty, attestationData, duty.DutySlot(), spectypes.DomainAttester)
			if err != nil {
				err := errors.Wrap(err, "failed signing attestation data")
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

			// TODO: revert log
			attDataRoot, err := attestationData.HashTreeRoot()
			if err != nil {
				err := errors.Wrap(err, "failed to hash attestation data")
				span.SetStatus(codes.Error, err.Error())
				return err
			}

			eventMsg := "signed attestation data"

			span.AddEvent(eventMsg, trace.WithAttributes(
				attribute.String("ssv.validator.signing_root", hex.EncodeToString(partialMsg.SigningRoot[:])),
				attribute.String("ssv.validator.signature", hex.EncodeToString(partialMsg.PartialSignature[:])),
				attribute.String("ssv.validator.attestation_data_root", hex.EncodeToString(attDataRoot[:])),
			))
		case spectypes.BNRoleSyncCommittee:
			validDuties++
			blockRoot := beaconVote.BlockRoot
			partialMsg, err := cr.BaseRunner.signBeaconObject(cr, duty, spectypes.SSZBytes(blockRoot[:]), duty.DutySlot(), spectypes.DomainSyncCommittee)
			if err != nil {
				err := errors.Wrap(err, "failed signing sync committee message")
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)
		default:
			err := fmt.Errorf("invalid duty type: %s", duty.Type)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	if validDuties == 0 {
		cr.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDuties.Error())
		return ErrNoValidDuties
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID: spectypes.NewMsgID(
			cr.BaseRunner.DomainType,
			cr.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:],
			cr.BaseRunner.RunnerRoleType,
		),
	}
	ssvMsg.Data, err = postConsensusMsg.Encode()
	if err != nil {
		err = errors.Wrap(err, "failed to encode post consensus signature msg")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	sig, err := cr.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		err = errors.Wrap(err, "could not sign SSVMessage")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{cr.BaseRunner.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	if err := cr.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		err = errors.Wrap(err, "can't broadcast partial post consensus sig")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// TODO finish edge case where some roots may be missing
func (cr *CommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("%s.runner.process_post_consensus", observabilityNamespace),
		trace.WithAttributes(
			attribute.Int64("ssv.validator.duty.slot", int64(signedMsg.Slot)),
			attribute.Int64("ssv.validator.signer", int64(signedMsg.Messages[0].Signer)),
			attribute.Int64("ssv.validator.msg_type", int64(signedMsg.Type)),
		))
	defer span.End()

	quorum, roots, err := cr.BaseRunner.basePostConsensusMsgProcessing(logger, cr, signedMsg)
	span.SetAttributes(
		attribute.Bool("ssv.validator.quorum", quorum),
		attribute.Int("ssv.validator.signatures", len(roots)),
	)
	if err != nil {
		err := errors.Wrap(err, "failed processing post consensus message")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	logger = logger.With(fields.Slot(signedMsg.Slot))

	// TODO: (Alan) revert?
	indices := make([]uint64, len(signedMsg.Messages))
	signers := make([]uint64, len(signedMsg.Messages))
	for i, msg := range signedMsg.Messages {
		signers[i] = msg.Signer
		indices[i] = uint64(msg.ValidatorIndex)
	}
	logger = logger.With(fields.ConsensusTime(cr.measurements.ConsensusTime()))

	eventMsg := "got partial signatures"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		zap.Bool("quorum", quorum),
		fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Int("sigs", len(roots)),
		zap.Uint64s("validators", indices))

	if !quorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := cr.expectedPostConsensusRootsAndBeaconObjects(logger)
	if err != nil {
		err := errors.Wrap(err, "could not get expected post consensus roots and beacon objects")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if len(beaconObjects) == 0 {
		cr.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDuties.Error())
		return ErrNoValidDuties
	}

	var anyErr error
	attestationsToSubmit := make(map[phase0.ValidatorIndex]*phase0.Attestation)
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
		eventMsg := "found validators for root"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconRoleAttribute(role),
			attribute.String("ssv.validator.duty.root", hex.EncodeToString(root[:])),
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
				eventMsg = "got post-consensus quorum but it has invalid signatures"
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

			eventMsg = "reconstructed partial signatures committee"
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
				att := sszObject.(*phase0.Attestation)
				// Insert signature
				att.Signature = specSig

				attestationsToSubmit[validator] = att
			}
		}
	}

	cr.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, cr.measurements.PostConsensusTime(), spectypes.RoleCommittee)

	logger = logger.With(fields.PostConsensusTime(cr.measurements.PostConsensusTime()))

	// Submit multiple attestations
	attestations := make([]*phase0.Attestation, 0, len(attestationsToSubmit))
	for _, att := range attestationsToSubmit {
		attestations = append(attestations, att)
	}

	cr.measurements.EndDutyFlow()

	if len(attestations) > 0 {
		submissionStart := time.Now()
		if err := cr.beacon.SubmitAttestations(attestations); err != nil {
			logger.Error("❌ failed to submit attestation", zap.Error(err))
			recordFailedSubmission(ctx, spectypes.BNRoleAttester)
			err := errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		recordDutyDuration(ctx, cr.measurements.DutyDurationTime(), spectypes.BNRoleAttester, cr.BaseRunner.State.RunningInstance.State.Round)
		attestationsCount := len(attestations)
		if attestationsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(ctx,
				uint32(attestationsCount),
				cr.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleAttester)
		}
		eventMsg := "✅ successfully submitted attestations"
		span.AddEvent(eventMsg, trace.WithAttributes(
			attribute.Int64("ssv.validator.duty.height", int64(cr.BaseRunner.QBFTController.Height)),
			attribute.Int64("ssv.validator.duty.round", int64(cr.BaseRunner.State.RunningInstance.State.Round)),
			attribute.String("ssv.validator.duty.block_root", hex.EncodeToString(attestations[0].Data.BeaconBlockRoot[:])),
			attribute.Float64("ssv.validator.duty.submission_time", time.Since(submissionStart).Seconds()),
			attribute.Float64("ssv.validator.duty.consensus_time_total", time.Since(cr.started).Seconds()),
		))
		logger.Info(eventMsg,
			fields.Height(cr.BaseRunner.QBFTController.Height),
			fields.Round(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(attestations[0].Data.BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(time.Since(cr.measurements.consensusStart)))

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
		if err := cr.beacon.SubmitSyncMessages(syncCommitteeMessages); err != nil {
			logger.Error("❌ failed to submit sync committee", zap.Error(err))
			recordFailedSubmission(ctx, spectypes.BNRoleSyncCommittee)
			err = errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		recordDutyDuration(ctx, cr.measurements.DutyDurationTime(), spectypes.BNRoleSyncCommittee, cr.BaseRunner.State.RunningInstance.State.Round)
		syncMsgsCount := len(syncCommitteeMessages)
		if syncMsgsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(ctx,
				uint32(syncMsgsCount),
				cr.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleSyncCommittee)
		}
		eventMsg := "✅ successfully submitted sync committee"
		span.AddEvent(eventMsg, trace.WithAttributes(
			attribute.Int64("ssv.validator.duty.height", int64(cr.BaseRunner.QBFTController.Height)),
			attribute.Int64("ssv.validator.duty.round", int64(cr.BaseRunner.State.RunningInstance.State.Round)),
			attribute.String("ssv.validator.duty.block_root", hex.EncodeToString(attestations[0].Data.BeaconBlockRoot[:])),
			attribute.Float64("ssv.validator.duty.submission_time", time.Since(submissionStart).Seconds()),
			attribute.Float64("ssv.validator.duty.consensus_time_total", time.Since(cr.started).Seconds()),
		))
		logger.Info(eventMsg,
			fields.Height(cr.BaseRunner.QBFTController.Height),
			fields.Round(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(syncCommitteeMessages[0].BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(time.Since(cr.measurements.consensusStart)))

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
func (cr CommitteeRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("expected post consensus roots function is unused")
}

func (cr *CommitteeRunner) expectedPostConsensusRootsAndBeaconObjects(logger *zap.Logger) (
	attestationMap map[phase0.ValidatorIndex][32]byte,
	syncCommitteeMap map[phase0.ValidatorIndex][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]ssz.HashRoot, error error,
) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]ssz.HashRoot)
	duty := cr.BaseRunner.State.StartingDuty
	// TODO DecidedValue should be interface??
	beaconVoteData := cr.BaseRunner.State.DecidedValue
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(beaconVoteData); err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not decode beacon vote")
	}

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
		epoch := cr.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
		switch validatorDuty.Type {
		case spectypes.BNRoleAttester:
			// Attestation object
			attestationData := constructAttestationData(beaconVote, validatorDuty)
			aggregationBitfield := bitfield.NewBitlist(validatorDuty.CommitteeLength)
			aggregationBitfield.SetBitAt(validatorDuty.ValidatorCommitteeIndex, true)
			unSignedAtt := &phase0.Attestation{
				Data:            attestationData,
				AggregationBits: aggregationBitfield,
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(epoch, spectypes.DomainAttester)
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
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]ssz.HashRoot)
			}
			beaconObjects[validatorDuty.ValidatorIndex][root] = unSignedAtt
		case spectypes.BNRoleSyncCommittee:
			// Sync committee beacon object
			syncMsg := &altair.SyncCommitteeMessage{
				Slot:            slot,
				BeaconBlockRoot: beaconVote.BlockRoot,
				ValidatorIndex:  validatorDuty.ValidatorIndex,
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(epoch, spectypes.DomainSyncCommittee)
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
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]ssz.HashRoot)
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
		fmt.Sprintf("%s.runner.execute_duty", observabilityNamespace),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			attribute.Int64("ssv.validator.duty.slot", int64(duty.DutySlot()))))
	defer span.End()

	cr.measurements.StartDutyFlow()
    
	start := time.Now()
	slot := duty.DutySlot()
	// We set committeeIndex to 0 for simplicity, there is no need to specify it exactly because
	// all 64 Ethereum committees assigned to this slot will get the same data to attest for.
	attData, _, err := cr.GetBeaconNode().GetAttestationData(slot, 0)
	if err != nil {
		err := errors.Wrap(err, "failed to get attestation data")
		span.SetStatus(codes.Error, err.Error())
		return err
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
		err := errors.Wrap(err, "can't start new duty runner instance for duty")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func (cr *CommitteeRunner) GetSigner() spectypes.BeaconSigner {
	return cr.signer
}

func (cr *CommitteeRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return cr.operatorSigner
}

func constructAttestationData(vote *spectypes.BeaconVote, duty *spectypes.ValidatorDuty) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            duty.Slot,
		Index:           duty.CommitteeIndex,
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
}
