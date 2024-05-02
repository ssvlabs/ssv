package runner

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner/metrics"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"go.uber.org/zap"
)

type CommitteeRunner struct {
	BaseRunner     *BaseRunner
	beacon         specssv.BeaconNode
	network        specssv.Network
	signer         spectypes.BeaconSigner
	operatorSigner spectypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF
	started        time.Time
	metrics        metrics.ConsensusMetrics
}

func NewCommitteeRunner(beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network specssv.Network,
	signer spectypes.BeaconSigner,
	operatorSigner spectypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
) Runner {
	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleCommittee,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
			QBFTController: qbftController,
		},
		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		valCheck:       valCheck,
		metrics:        metrics.NewConsensusMetrics(spectypes.RoleCommittee),
	}
}

func (r *CommitteeRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

func (r *CommitteeRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *CommitteeRunner) GetBeaconNode() specssv.BeaconNode {
	return r.beacon
}

func (r *CommitteeRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

// StopDuty stops the duty for the given validator
func (r *CommitteeRunner) StopDuty(validator spectypes.ValidatorPK) {
	for _, duty := range r.BaseRunner.State.StartingDuty.(*spectypes.CommitteeDuty).BeaconDuties {
		if spectypes.ValidatorPK(duty.PubKey) == validator {
			duty.IsStopped = true
		}
	}
}

func (r *CommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *CommitteeRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

func (r *CommitteeRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (r *CommitteeRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *CommitteeRunner) GetNetwork() specssv.Network {
	return r.network
}

func (r *CommitteeRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre consensus phase for committee runner")
}

func (r *CommitteeRunner) ProcessConsensus(logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, msg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	if err != nil {
		return errors.Wrap(err, "decided value is not a beacon vote")
	}

	duty := r.BaseRunner.State.StartingDuty
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	beaconVote := decidedValue.(*spectypes.BeaconVote)
	for _, duty := range duty.(*spectypes.CommitteeDuty).BeaconDuties {
		switch duty.Type {
		case spectypes.BNRoleAttester:
			attestationData := constructAttestationData(beaconVote, duty)

			partialMsg, err := r.BaseRunner.signBeaconObject(r, duty, attestationData, duty.DutySlot(),
				spectypes.DomainAttester)
			if err != nil {
				return errors.Wrap(err, "failed signing attestation data")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

		case spectypes.BNRoleSyncCommittee:
			syncCommitteeMessage := ConstructSyncCommittee(beaconVote, duty)
			partialMsg, err := r.BaseRunner.signBeaconObject(r, duty, syncCommitteeMessage, duty.DutySlot(),
				spectypes.DomainSyncCommittee)
			if err != nil {
				return errors.Wrap(err, "failed signing sync committee message")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)
		}
	}

	data, err := postConsensusMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		//TODO: The Domain will be updated after new Domain PR... Will be created after this PR is merged
		MsgID: spectypes.NewMsgID(spectypes.GenesisMainnet, r.GetBaseRunner().QBFTController.Share.ClusterID[:],
			r.BaseRunner.RunnerRoleType),
		Data: data,
	}

	msgToBroadcast, err := spectypes.SSVMessageToSignedSSVMessage(ssvMsg, r.BaseRunner.QBFTController.Share.OperatorID,
		r.operatorSigner.SignSSVMessage)
	if err != nil {
		return errors.Wrap(err, "could not create SignedSSVMessage from SSVMessage")
	}

	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil

}

// TODO finish edge case where some roots may be missing
func (r *CommitteeRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)

	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}
	attestationMap, committeeMap, beaconObjects, err := r.expectedPostConsensusRootsAndBeaconObjects()
	if err != nil {
		return errors.Wrap(err, "could not get expected post consensus roots and beacon objects")
	}
	for _, root := range roots {
		role, validators, found := findValidators(root, attestationMap, committeeMap)

		if !found {
			// TODO error?
			continue
		}

		for _, validator := range validators {
			share := r.BaseRunner.Share[validator]
			pubKey := share.ValidatorPubKey
			sig, err := r.BaseRunner.State.ReconstructBeaconSig(r.BaseRunner.State.PostConsensusContainer, root,
				pubKey[:], validator)
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			// TODO should we return an error here? maybe other sigs are fine?
			if err != nil {
				for _, root := range roots {
					r.BaseRunner.FallBackAndVerifyEachSignature(r.BaseRunner.State.PostConsensusContainer, root,
						share.Committee, validator)
				}
				return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
			}
			specSig := phase0.BLSSignature{}
			copy(specSig[:], sig)

			if role == spectypes.BNRoleAttester {
				att := beaconObjects[root].(*phase0.Attestation)
				att.Signature = specSig
				// broadcast
				if err := r.beacon.SubmitAttestation(att); err != nil {
					return errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
				}
			} else if role == spectypes.BNRoleSyncCommittee {
				syncMsg := beaconObjects[root].(*altair.SyncCommitteeMessage)
				syncMsg.Signature = specSig
				if err := r.beacon.SubmitSyncMessage(syncMsg); err != nil {
					return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
				}
			}
		}

	}
	r.BaseRunner.State.Finished = true
	return nil
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
			return spectypes.BNRoleSyncCommittee, []phase0.ValidatorIndex{validator}, true
		}
	}
	return spectypes.BNRoleUnknown, nil, false
}

// unneeded
func (r *CommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no pre consensus root for committee runner")
}

// This function signature returns only one domain type
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (r *CommitteeRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{}, spectypes.DomainCommittee, nil
}

func (cr *CommitteeRunner) expectedPostConsensusRootsAndBeaconObjects() (attestationMap map[phase0.ValidatorIndex][32]byte,
	syncCommitteeMap map[phase0.ValidatorIndex][32]byte, beaconObjects map[[32]byte]ssz.HashRoot, error error) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	duty := cr.BaseRunner.State.StartingDuty
	// TODO DecidedValue should be interface??
	beaconVoteData := cr.BaseRunner.State.DecidedValue
	beaconVote, err := spectypes.NewBeaconVote(beaconVoteData)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not decode beacon vote")
	}
	beaconVote.Decode(beaconVoteData)
	for _, beaconDuty := range duty.(*spectypes.CommitteeDuty).BeaconDuties {
		if beaconDuty == nil || beaconDuty.IsStopped {
			continue
		}
		switch beaconDuty.Type {
		case spectypes.BNRoleAttester:
			attestationData := constructAttestationData(beaconVote, beaconDuty)
			aggregationBitfield := bitfield.NewBitlist(beaconDuty.CommitteeLength)
			aggregationBitfield.SetBitAt(beaconDuty.ValidatorCommitteeIndex, true)
			unSignedAtt := &phase0.Attestation{
				Data:            attestationData,
				AggregationBits: aggregationBitfield,
			}
			root, _ := attestationData.HashTreeRoot()
			attestationMap[beaconDuty.ValidatorIndex] = root
			beaconObjects[root] = unSignedAtt
		case spectypes.BNRoleSyncCommittee:
			syncCommitteeMessage := ConstructSyncCommittee(beaconVote, beaconDuty)
			root, _ := syncCommitteeMessage.HashTreeRoot()
			syncCommitteeMap[beaconDuty.ValidatorIndex] = root
			beaconObjects[root] = syncCommitteeMessage
		}
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

func (r *CommitteeRunner) executeDuty(logger *zap.Logger, duty spectypes.Duty) error {
	//TODO committeeIndex is 0, is this correct?
	attData, ver, err := r.GetBeaconNode().GetAttestationData(duty.DutySlot(), 0)
	if err != nil {
		return errors.Wrap(err, "failed to get attestation data")
	}

	vote := spectypes.BeaconVote{
		BlockRoot: attData.BeaconBlockRoot,
		Source:    attData.Source,
		Target:    attData.Target,
	}
	voteByts, err := vote.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal attestation data")
	}

	//TODO should duty be empty?
	input := &spectypes.ConsensusData{
		Duty:    spectypes.BeaconDuty{},
		Version: ver,
		DataSSZ: voteByts,
	}

	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}

func (r *CommitteeRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}

func (r *CommitteeRunner) GetOperatorSigner() spectypes.OperatorSigner {
	return r.operatorSigner
}

func constructAttestationData(vote *spectypes.BeaconVote, duty *spectypes.BeaconDuty) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            duty.DutySlot(),
		Index:           duty.CommitteeIndex,
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
}
func ConstructSyncCommittee(vote *spectypes.BeaconVote, duty *spectypes.BeaconDuty) *altair.SyncCommitteeMessage {
	return &altair.SyncCommitteeMessage{
		Slot:            duty.DutySlot(),
		BeaconBlockRoot: vote.BlockRoot,
		ValidatorIndex:  duty.ValidatorIndex,
	}
}
