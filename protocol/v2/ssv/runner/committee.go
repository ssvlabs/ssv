package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/metrics"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
)

//type Broadcaster interface {
//	Broadcast(msg *types.SignedSSVMessage) error
//}
//
//type BeaconNode interface {
//	DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error)
//	SubmitAttestation(attestation *phase0.Attestation) error
//}

type CommitteeRunner struct {
	BaseRunner     *BaseRunner
	network        specqbft.Network
	beacon         beacon.BeaconNode
	signer         types.BeaconSigner
	operatorSigner types.OperatorSigner
	domain         spectypes.DomainType
	valCheck       specqbft.ProposedValueCheckF

	stoppedValidators map[spectypes.ValidatorPK]struct{}
	submittedDuties   map[types.BeaconRole]map[phase0.ValidatorIndex]struct{}

	started time.Time
	metrics metrics.ConsensusMetrics
}

func NewCommitteeRunner(
	networkConfig networkconfig.NetworkConfig,
	share map[phase0.ValidatorIndex]*types.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer types.BeaconSigner,
	operatorSigner types.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
) Runner {
	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     types.RoleCommittee,
			DomainTypeProvider: networkConfig,
			BeaconNetwork:      networkConfig.Beacon.GetBeaconNetwork(),
			Share:              share,
			QBFTController:     qbftController,
		},
		beacon:            beacon,
		network:           network,
		signer:            signer,
		operatorSigner:    operatorSigner,
		valCheck:          valCheck,
		stoppedValidators: make(map[spectypes.ValidatorPK]struct{}),
		submittedDuties:   make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}),
		metrics:           metrics.NewConsensusMetrics(spectypes.RoleCommittee),
	}
}

func (cr *CommitteeRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	err := cr.BaseRunner.baseStartNewDuty(logger, cr, duty, quorum)
	if err != nil {
		return err
	}
	cr.submittedDuties[types.BNRoleAttester] = make(map[phase0.ValidatorIndex]struct{})
	cr.submittedDuties[types.BNRoleSyncCommittee] = make(map[phase0.ValidatorIndex]struct{})
	return nil
}

func (cr *CommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(cr)
}

// StopDuty stops the duty for the given validator
func (cr *CommitteeRunner) StopDuty(validator types.ValidatorPK) {
	cr.stoppedValidators[validator] = struct{}{}
}

func (cr *CommitteeRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &cr)
}

func (cr *CommitteeRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := cr.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (cr *CommitteeRunner) MarshalJSON() ([]byte, error) {
	type CommitteeAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         types.BeaconSigner
		operatorSigner types.OperatorSigner
		valCheck       specqbft.ProposedValueCheckF
	}

	// Create object and marshal
	alias := &CommitteeAlias{
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
	type CommitteeAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         types.BeaconSigner
		operatorSigner types.OperatorSigner
		valCheck       specqbft.ProposedValueCheckF
		//
		//stoppedValidators map[spectypes.ValidatorPK]struct{}
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeAlias{}
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
	//cr.stoppedValidators = aux.stoppedValidators
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

func (cr *CommitteeRunner) GetBeaconSigner() types.BeaconSigner {
	return cr.signer
}

func (cr *CommitteeRunner) HasRunningDuty() bool {
	return cr.BaseRunner.hasRunningDuty()
}

func (cr *CommitteeRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *types.PartialSignatureMessages) error {
	return errors.New("no pre consensus phase for committee runner")
}

func (cr *CommitteeRunner) ProcessConsensus(logger *zap.Logger, msg *types.SignedSSVMessage) error {
	decided, decidedValue, err := cr.BaseRunner.baseConsensusMsgProcessing(logger, cr, msg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	cr.metrics.EndConsensus()
	cr.metrics.StartPostConsensus()
	// decided means consensus is done

	duty := cr.BaseRunner.State.StartingDuty
	postConsensusMsg := &types.PartialSignatureMessages{
		Type:     types.PostConsensusPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*types.PartialSignatureMessage{},
	}

	beaconVote := decidedValue.(*types.BeaconVote)
	for _, duty := range duty.(*types.CommitteeDuty).BeaconDuties {
		switch duty.Type {
		case types.BNRoleAttester:
			attestationData := constructAttestationData(beaconVote, duty)
			err = cr.GetSigner().IsAttestationSlashable(cr.GetBaseRunner().Share[duty.ValidatorIndex].SharePubKey,
				attestationData)
			if err != nil {
				return errors.Wrap(err, "attempting to sign slashable attestation data")
			}
			partialMsg, err := cr.BaseRunner.signBeaconObject(cr, duty, attestationData, duty.DutySlot(),
				types.DomainAttester)
			if err != nil {
				return errors.Wrap(err, "failed signing attestation data")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

			// TODO: revert log
			adr, err := attestationData.HashTreeRoot()
			if err != nil {
				return errors.Wrap(err, "failed to hash attestation data")
			}
			logger.Debug("signed attestation data", zap.Int("validator_index", int(duty.ValidatorIndex)),
				zap.String("pub_key", hex.EncodeToString(duty.PubKey[:])),
				zap.Any("attestation_data", attestationData),
				zap.String("attestation_data_root", hex.EncodeToString(adr[:])),
				zap.String("signing_root", hex.EncodeToString(partialMsg.SigningRoot[:])),
				zap.String("signature", hex.EncodeToString(partialMsg.PartialSignature[:])),
			)
		case types.BNRoleSyncCommittee:
			blockRoot := beaconVote.BlockRoot
			partialMsg, err := cr.BaseRunner.signBeaconObject(cr, duty, types.SSZBytes(blockRoot[:]), duty.DutySlot(),
				types.DomainSyncCommittee)
			if err != nil {
				return errors.Wrap(err, "failed signing sync committee message")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)
		}
	}

	ssvMsg := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID: types.NewMsgID(
			cr.BaseRunner.DomainTypeProvider.DomainType(),
			cr.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:],
			cr.BaseRunner.RunnerRoleType,
		),
	}
	ssvMsg.Data, err = postConsensusMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast, err := types.SSVMessageToSignedSSVMessage(ssvMsg, cr.BaseRunner.QBFTController.CommitteeMember.OperatorID,
		cr.operatorSigner.SignSSVMessage)
	if err != nil {
		return errors.Wrap(err, "could not create SignedSSVMessage from SSVMessage")
	}

	if err := cr.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil

}

// TODO finish edge case where some roots may be missing
func (cr *CommitteeRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *types.PartialSignatureMessages) error {
	quorum, roots, err := cr.BaseRunner.basePostConsensusMsgProcessing(logger, cr, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}
	logger = logger.With(fields.Slot(signedMsg.Slot))

	// TODO: (Alan) revert?
	indices := make([]int, len(signedMsg.Messages))
	signers := make([]uint64, len(signedMsg.Messages))
	for i, msg := range signedMsg.Messages {
		signers[i] = msg.Signer
		indices[i] = int(msg.ValidatorIndex)
	}

	logger.Debug("🧩 got partial signatures",
		zap.Bool("quorum", quorum),
		fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
		zap.Int("signer", int(signedMsg.Messages[0].Signer)),
		zap.Int("sigs", len(roots)),
		zap.Ints("validators", indices))

	if !quorum {
		return nil
	}
	durationFields := []zap.Field{
		fields.ConsensusTime(cr.metrics.GetConsensusTime()),
	}
	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := cr.expectedPostConsensusRootsAndBeaconObjects()
	if err != nil {
		return errors.Wrap(err, "could not get expected post consensus roots and beacon objects")
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
			// Check if duty has terminated (runner has submitted for all duties)
			if cr.HasSubmittedAllBeaconDuties(attestationMap, committeeMap) {
				cr.BaseRunner.State.Finished = true
			}
			// All roots have quorum, so if we can't find validators for a root, it means we have a bug
			// We assume it is safe to stop due to honest majority assumption
			return errors.New("could not find validators for root")
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

			//validator := validator
			// Reconstruct signature
			share := cr.BaseRunner.Share[validator]
			pubKey := share.ValidatorPubKey
			vlogger := logger.With(zap.Int("validator_index", int(validator)), zap.String("pubkey", hex.EncodeToString(pubKey[:])))
			vlogger = vlogger.With(durationFields...)

			sig, err := cr.BaseRunner.State.ReconstructBeaconSig(cr.BaseRunner.State.PostConsensusContainer, root,
				pubKey[:], validator)
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			// TODO should we return an error here? maybe other sigs are fine?
			if err != nil {
				for _, root := range roots {
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
			if _, exists := beaconObjects[validator]; !exists {
				anyErr = errors.Wrap(err, "could not find beacon object for validator")
				continue
			}
			if _, exists := beaconObjects[validator][root]; !exists {
				anyErr = errors.Wrap(err, "could not find beacon object for validator")
				continue
			}

			sszObject := beaconObjects[validator][root]

			// Store objects for multiple submission
			if role == types.BNRoleSyncCommittee {
				syncMsg := sszObject.(*altair.SyncCommitteeMessage)
				// Insert signature
				syncMsg.Signature = specSig

				syncCommitteeMessagesToSubmit[validator] = syncMsg

			} else if role == types.BNRoleAttester {
				att := sszObject.(*phase0.Attestation)
				// Insert signature
				att.Signature = specSig

				attestationsToSubmit[validator] = att
			}
		}
	}
	cr.metrics.EndPostConsensus()
	durationFields = append(durationFields, fields.PostConsensusTime(cr.metrics.GetPostConsensusTime()))
	logger = logger.With(durationFields...)
	// Submit multiple attestations
	attestations := make([]*phase0.Attestation, 0)
	for _, att := range attestationsToSubmit {
		attestations = append(attestations, att)
	}
	submmitionStart := time.Now()
	if err := cr.beacon.SubmitAttestations(attestations); err != nil {
		logger.Error("❌ failed to submit attestation", zap.Error(err))
		return errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
	}

	if len(attestations) > 0 {
		logger.Info("✅ successfully submitted attestations",
			fields.Height(cr.BaseRunner.QBFTController.Height),
			fields.Round(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.Root([32]byte(attestations[0].Data.BeaconBlockRoot[:])),
			fields.SubmissionTime(time.Since(submmitionStart)),
			zap.Duration("total_consensus_time", time.Since(cr.started)))
	}
	// Record successful submissions
	for validator := range attestationsToSubmit {
		cr.RecordSubmission(types.BNRoleAttester, validator)
	}

	// Submit multiple sync committee
	syncCommitteeMessages := make([]*altair.SyncCommitteeMessage, 0)
	for _, syncMsg := range syncCommitteeMessagesToSubmit {
		syncCommitteeMessages = append(syncCommitteeMessages, syncMsg)
	}
	submmitionStart = time.Now()
	if err := cr.beacon.SubmitSyncMessages(syncCommitteeMessages); err != nil {
		logger.Error("❌ failed to submit sync committee", zap.Error(err))
		return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
	}
	if len(syncCommitteeMessages) > 0 {
		logger.Info("✅ successfully submitted sync committee",
			fields.Height(cr.BaseRunner.QBFTController.Height),
			fields.Round(cr.BaseRunner.State.RunningInstance.State.Round),
			fields.Root([32]byte(syncCommitteeMessages[0].BeaconBlockRoot[:])),
			fields.SubmissionTime(time.Since(submmitionStart)),
			zap.Duration("total_consensus_time", time.Since(cr.started)))
	}
	// Record successful submissions
	for validator := range syncCommitteeMessagesToSubmit {
		cr.RecordSubmission(types.BNRoleSyncCommittee, validator)
	}

	if anyErr != nil {
		return anyErr
	}

	// Check if duty has terminated (runner has submitted for all duties)
	if cr.HasSubmittedAllBeaconDuties(attestationMap, committeeMap) {
		cr.BaseRunner.State.Finished = true
	}
	return nil
}

// HasSubmittedAllBeaconDuties -- Returns true if the runner has done submissions for all validators for the given slot
func (cr *CommitteeRunner) HasSubmittedAllBeaconDuties(attestationMap map[phase0.ValidatorIndex][32]byte, syncCommitteeMap map[phase0.ValidatorIndex][32]byte) bool {
	// Expected total
	expectedTotalSubmissions := len(attestationMap) + len(syncCommitteeMap)

	totalSubmissions := 0

	// Add submitted attestation duties
	for valIdx := range attestationMap {
		if cr.HasSubmitted(types.BNRoleAttester, valIdx) {
			totalSubmissions++
		}
	}
	// Add submitted sync committee duties
	for valIdx := range syncCommitteeMap {
		if cr.HasSubmitted(types.BNRoleSyncCommittee, valIdx) {
			totalSubmissions++
		}
	}
	return totalSubmissions >= expectedTotalSubmissions
}

// RecordSubmission -- Records a submission for the (role, validator index, slot) tuple
func (cr *CommitteeRunner) RecordSubmission(role types.BeaconRole, valIdx phase0.ValidatorIndex) {
	if _, ok := cr.submittedDuties[role]; !ok {
		cr.submittedDuties[role] = make(map[phase0.ValidatorIndex]struct{})
	}
	cr.submittedDuties[role][valIdx] = struct{}{}
}

// HasSubmitted -- Returns true if there is a record of submission for the (role, validator index, slot) tuple
func (cr *CommitteeRunner) HasSubmitted(role types.BeaconRole, valIdx phase0.ValidatorIndex) bool {
	if _, ok := cr.submittedDuties[role]; !ok {
		return false
	}
	_, ok := cr.submittedDuties[role][valIdx]
	return ok
}

func findValidators(
	expectedRoot [32]byte,
	attestationMap map[phase0.ValidatorIndex][32]byte,
	committeeMap map[phase0.ValidatorIndex][32]byte) (types.BeaconRole, []phase0.ValidatorIndex, bool) {
	var validators []phase0.ValidatorIndex

	// look for the expectedRoot in attestationMap
	for validator, root := range attestationMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return types.BNRoleAttester, validators, true
	}
	// look for the expectedRoot in committeeMap
	for validator, root := range committeeMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return types.BNRoleSyncCommittee, validators, true
	}
	return types.BNRoleUnknown, nil, false
}

// unneeded
func (cr CommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, types.DomainError, errors.New("no pre consensus root for committee runner")
}

// This function signature returns only one domain type
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (cr CommitteeRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{}, types.DomainAttester, nil
}

func (cr *CommitteeRunner) expectedPostConsensusRootsAndBeaconObjects() (
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
	beaconVote, err := types.NewBeaconVote(beaconVoteData)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not decode beacon vote")
	}
	err = beaconVote.Decode(beaconVoteData)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not decode beacon vote")
	}
	for _, beaconDuty := range duty.(*types.CommitteeDuty).BeaconDuties {
		_, stopped := cr.stoppedValidators[spectypes.ValidatorPK(beaconDuty.PubKey)]
		if beaconDuty == nil || stopped {
			continue
		}
		slot := beaconDuty.DutySlot()
		epoch := cr.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
		switch beaconDuty.Type {
		case types.BNRoleAttester:

			// Attestation object
			attestationData := constructAttestationData(beaconVote, beaconDuty)
			aggregationBitfield := bitfield.NewBitlist(beaconDuty.CommitteeLength)
			aggregationBitfield.SetBitAt(beaconDuty.ValidatorCommitteeIndex, true)
			unSignedAtt := &phase0.Attestation{
				Data:            attestationData,
				AggregationBits: aggregationBitfield,
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(epoch, types.DomainAttester)
			if err != nil {
				continue
			}
			root, err := types.ComputeETHSigningRoot(attestationData, domain)
			if err != nil {
				continue
			}

			// Add to map
			attestationMap[beaconDuty.ValidatorIndex] = root
			if _, ok := beaconObjects[beaconDuty.ValidatorIndex]; !ok {
				beaconObjects[beaconDuty.ValidatorIndex] = make(map[[32]byte]ssz.HashRoot)
			}
			beaconObjects[beaconDuty.ValidatorIndex][root] = unSignedAtt
		case types.BNRoleSyncCommittee:
			// Sync committee beacon object
			syncMsg := &altair.SyncCommitteeMessage{
				Slot:            slot,
				BeaconBlockRoot: beaconVote.BlockRoot,
				ValidatorIndex:  beaconDuty.ValidatorIndex,
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(epoch, types.DomainSyncCommittee)
			if err != nil {
				continue
			}
			// Eth root
			blockRoot := types.SSZBytes(beaconVote.BlockRoot[:])
			root, err := types.ComputeETHSigningRoot(blockRoot, domain)
			if err != nil {
				continue
			}

			// Set root and beacon object
			syncCommitteeMap[beaconDuty.ValidatorIndex] = root
			if _, ok := beaconObjects[beaconDuty.ValidatorIndex]; !ok {
				beaconObjects[beaconDuty.ValidatorIndex] = make(map[[32]byte]ssz.HashRoot)
			}
			beaconObjects[beaconDuty.ValidatorIndex][root] = syncMsg
		}
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

func (cr *CommitteeRunner) executeDuty(logger *zap.Logger, duty types.Duty) error {
	start := time.Now()
	slot := duty.DutySlot()
	attData, _, err := cr.GetBeaconNode().GetAttestationData(slot, 0)
	if err != nil {
		return errors.Wrap(err, "failed to get attestation data")
	}
	//TODO committeeIndex is 0, is this correct?
	logger = logger.With(
		zap.Duration("attestation_data_time", time.Since(start)),
		fields.Slot(slot),
	)

	cr.started = time.Now()
	cr.metrics.StartConsensus()

	vote := types.BeaconVote{
		BlockRoot: attData.BeaconBlockRoot,
		Source:    attData.Source,
		Target:    attData.Target,
	}
	voteByts, err := vote.Encode()
	if err != nil {
		return errors.Wrap(err, "could not marshal attestation data")
	}

	if err := cr.BaseRunner.decide(logger, cr, duty.DutySlot(), voteByts); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}

func (cr *CommitteeRunner) GetSigner() types.BeaconSigner {
	return cr.signer
}

func (cr *CommitteeRunner) GetOperatorSigner() types.OperatorSigner {
	return cr.operatorSigner
}

func constructAttestationData(vote *types.BeaconVote, duty *types.BeaconDuty) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            duty.Slot,
		Index:           duty.CommitteeIndex,
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
}
