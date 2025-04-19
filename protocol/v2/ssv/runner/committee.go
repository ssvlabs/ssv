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
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
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
	networkConfig networkconfig.NetworkConfig,
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
			DomainType:     networkConfig.DomainType,
			BeaconNetwork:  networkConfig.Beacon.GetBeaconNetwork(),
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
	d, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		return errors.New("duty is not a CommitteeDuty")
	}
	for _, validatorDuty := range d.ValidatorDuties {
		err := cr.DutyGuard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), d.DutySlot())
		if err != nil {
			return fmt.Errorf("could not start %s duty at slot %d for validator %x: %w",
				validatorDuty.Type, d.DutySlot(), validatorDuty.PubKey, err)
		}
	}
	err := cr.BaseRunner.baseStartNewDuty(ctx, logger, cr, duty, quorum)
	if err != nil {
		return err
	}
	cr.submittedDuties[spectypes.BNRoleAttester] = make(map[phase0.ValidatorIndex]struct{})
	cr.submittedDuties[spectypes.BNRoleSyncCommittee] = make(map[phase0.ValidatorIndex]struct{})
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
	decided, decidedValue, err := cr.BaseRunner.baseConsensusMsgProcessing(ctx, logger, cr, msg, &spectypes.BeaconVote{})
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
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

	epoch := cr.beacon.GetBeaconNetwork().EstimatedEpochAtSlot(duty.DutySlot())
	version := cr.beacon.DataVersion(epoch)

	for _, validatorDuty := range duty.(*spectypes.CommitteeDuty).ValidatorDuties {
		if err := cr.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
			logger.Warn("duty is no longer valid", fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
			continue
		}

		switch validatorDuty.Type {
		case spectypes.BNRoleAttester:
			totalAttesterDuties++

			// Doppelganger protection applies only to attester duties since they are slashable.
			// Sync committee duties are not slashable, so they are always allowed.
			if !cr.doppelgangerHandler.CanSign(validatorDuty.ValidatorIndex) {
				logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(validatorDuty.ValidatorIndex))
				blockedAttesterDuties++
				continue
			}

			attestationData := constructAttestationData(beaconVote, validatorDuty, version)
			partialMsg, err := cr.BaseRunner.signBeaconObject(
				ctx,
				cr,
				validatorDuty,
				attestationData,
				validatorDuty.DutySlot(),
				spectypes.DomainAttester,
			)
			if err != nil {
				return errors.Wrap(err, "failed signing attestation data")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

			// TODO: revert log
			attDataRoot, err := attestationData.HashTreeRoot()
			if err != nil {
				return errors.Wrap(err, "failed to hash attestation data")
			}
			logger.Debug("signed attestation data",
				zap.Uint64("validator_index", uint64(validatorDuty.ValidatorIndex)),
				zap.String("pub_key", hex.EncodeToString(validatorDuty.PubKey[:])),
				zap.Any("attestation_data", attestationData),
				zap.String("attestation_data_root", hex.EncodeToString(attDataRoot[:])),
				zap.String("signing_root", hex.EncodeToString(partialMsg.SigningRoot[:])),
				zap.String("signature", hex.EncodeToString(partialMsg.PartialSignature[:])),
			)
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
				return errors.Wrap(err, "failed signing sync committee message")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

		default:
			return fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
		}
	}

	if totalAttesterDuties == 0 && totalSyncCommitteeDuties == 0 {
		cr.BaseRunner.State.Finished = true
		return ErrNoValidDuties
	}

	// Avoid sending an empty message if all attester duties were blocked due to Doppelganger protection
	// and no sync committee duties exist.
	//
	// We do not mark the state as finished here because post-consensus messages must still be processed,
	// allowing validators to be marked as safe once sufficient consensus is reached.
	if totalAttesterDuties == blockedAttesterDuties && totalSyncCommitteeDuties == 0 {
		logger.Debug("Skipping message broadcast: all attester duties blocked by Doppelganger protection, no sync committee duties.",
			zap.Int("attester_duties", totalAttesterDuties),
			zap.Int("blocked_attesters", blockedAttesterDuties))
		return nil
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
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	sig, err := cr.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign SSVMessage")
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{cr.BaseRunner.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	if err := cr.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}

	return nil
}

// TODO finish edge case where some roots may be missing
func (cr *CommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := cr.BaseRunner.basePostConsensusMsgProcessing(logger, cr, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
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

	logger.Debug("🧩 got partial signatures",
		zap.Bool("quorum", quorum),
		fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Int("sigs", len(roots)),
		zap.Uint64s("validators", indices))

	if !quorum {
		return nil
	}

	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := cr.expectedPostConsensusRootsAndBeaconObjects(logger)
	if err != nil {
		return errors.Wrap(err, "could not get expected post consensus roots and beacon objects")
	}
	if len(beaconObjects) == 0 {
		cr.BaseRunner.State.Finished = true
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

		logger.Debug("found validators for root",
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
				vlogger.Error("got post-consensus quorum but it has invalid signatures",
					fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
					zap.Error(err),
				)

				anyErr = errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
				continue
			}
			specSig := phase0.BLSSignature{}
			copy(specSig[:], sig)

			vlogger.Debug("🧩 reconstructed partial signatures committee",
				zap.Uint64s("signers", getPostConsensusCommitteeSigners(cr.BaseRunner.State, root)))
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
		attestations = append(attestations, att)
	}

	cr.measurements.EndDutyFlow()

	if len(attestations) > 0 {
		submissionStart := time.Now()
		if err := cr.beacon.SubmitAttestations(attestations); err != nil {
			logger.Error("❌ failed to submit attestation", zap.Error(err))
			recordFailedSubmission(ctx, spectypes.BNRoleAttester)
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
		}

		recordDutyDuration(ctx, cr.measurements.DutyDurationTime(), spectypes.BNRoleAttester, cr.BaseRunner.State.RunningInstance.State.Round)

		attestationsCount := len(attestations)
		if attestationsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(ctx,
				uint32(attestationsCount),
				cr.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleAttester)
		}

		attData, err := attestations[0].Data()
		if err != nil {
			return errors.Wrap(err, "could not get attestation data")
			// TODO return error?
		}
		logger.Info("✅ successfully submitted attestations",
			fields.Epoch(cr.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot())),
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
		if err := cr.beacon.SubmitSyncMessages(syncCommitteeMessages); err != nil {
			logger.Error("❌ failed to submit sync committee", zap.Error(err))
			recordFailedSubmission(ctx, spectypes.BNRoleSyncCommittee)
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
		}

		recordDutyDuration(ctx, cr.measurements.DutyDurationTime(), spectypes.BNRoleSyncCommittee, cr.BaseRunner.State.RunningInstance.State.Round)

		syncMsgsCount := len(syncCommitteeMessages)
		if syncMsgsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(ctx,
				uint32(syncMsgsCount),
				cr.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(cr.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleSyncCommittee)
		}

		logger.Info("✅ successfully submitted sync committee",
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
		return anyErr
	}

	// Check if duty has terminated (runner has submitted for all duties)
	if cr.HasSubmittedAllValidatorDuties(attestationMap, committeeMap) {
		cr.BaseRunner.State.Finished = true
	}
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
	epoch := cr.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)

	dataVersion := cr.beacon.DataVersion(epoch)

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
			attestationData := constructAttestationData(beaconVote, validatorDuty, dataVersion)
			attestationResponse, err := specssv.ConstructVersionedAttestationWithoutSignature(attestationData, dataVersion, validatorDuty)
			if err != nil {
				logger.Debug("failed to construct attestation", zap.Error(err))
				continue
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
	cr.measurements.StartDutyFlow()

	start := time.Now()
	slot := duty.DutySlot()

	attData, _, err := cr.GetBeaconNode().GetAttestationData(slot)
	if err != nil {
		return errors.Wrap(err, "failed to get attestation data")
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
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
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
