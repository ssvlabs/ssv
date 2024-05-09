package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	"github.com/ssvlabs/ssv-spec/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
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
	beacon         specssv.BeaconNode
	network        qbft.FutureSpecNetwork
	signer         types.BeaconSigner
	operatorSigner types.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF
}

func NewCommitteeRunner(beaconNetwork types.BeaconNetwork,
	share map[phase0.ValidatorIndex]*types.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network qbft.FutureSpecNetwork,
	signer types.BeaconSigner,
	operatorSigner types.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
) Runner {
	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: types.RoleCommittee,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
			QBFTController: qbftController,
		},
		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		valCheck:       valCheck,
	}
}

func (cr *CommitteeRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty) error {
	return cr.BaseRunner.baseStartNewDuty(logger, cr, duty)
}

func (cr *CommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(cr)
}

// StopDuty stops the duty for the given validator
func (cr *CommitteeRunner) StopDuty(validator types.ValidatorPK) {
	for _, duty := range cr.BaseRunner.State.StartingDuty.(*types.CommitteeDuty).BeaconDuties {
		if types.ValidatorPK(duty.PubKey) == validator {
			duty.IsStopped = true
		}
	}
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

func (cr *CommitteeRunner) GetBaseRunner() *BaseRunner {
	return cr.BaseRunner
}

func (cr *CommitteeRunner) GetBeaconNode() specssv.BeaconNode {
	return cr.beacon
}

func (cr *CommitteeRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return cr.valCheck
}

func (cr *CommitteeRunner) GetNetwork() qbft.FutureSpecNetwork {
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

			partialMsg, err := cr.BaseRunner.signBeaconObject(cr, duty, attestationData, duty.DutySlot(),
				types.DomainAttester)
			if err != nil {
				return errors.Wrap(err, "failed signing attestation data")
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, partialMsg)

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
		//TODO: The Domain will be updated after new Domain PR... Will be created after this PR is merged
		MsgID: types.NewMsgID(types.GenesisMainnet, cr.GetBaseRunner().QBFTController.Share.ClusterID[:],
			cr.BaseRunner.RunnerRoleType),
	}
	ssvMsg.Data, err = postConsensusMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast, err := types.SSVMessageToSignedSSVMessage(ssvMsg, cr.BaseRunner.QBFTController.Share.OperatorID,
		cr.operatorSigner.SignSSVMessage)
	if err != nil {
		return errors.Wrap(err, "could not create SignedSSVMessage from SSVMessage")
	}

	// TODO: revert?
	logger.Debug("📢 broadcasting post consensus message",
		fields.Slot(duty.DutySlot()),
		zap.Int("sigs", len(postConsensusMsg.Messages)),
	)

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

	// TODO: revert?
	indices := make([]int, len(signedMsg.Messages))
	for i, msg := range signedMsg.Messages {
		indices[i] = int(msg.ValidatorIndex)
	}
	logger.Debug("got post consensus",
		zap.Bool("quorum", quorum),
		fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
		zap.Int("sigs", len(roots)),
		zap.Ints("validators", indices),
	)

	if !quorum {
		return nil
	}
	attestationMap, committeeMap, beaconObjects, err := cr.expectedPostConsensusRootsAndBeaconObjects()
	if err != nil {
		return errors.Wrap(err, "could not get expected post consensus roots and beacon objects")
	}
	for _, root := range roots {
		role, validators, found := findValidators(root, attestationMap, committeeMap)
		logger.Debug("found validators for root",
			fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
			zap.String("role", role.String()),
			zap.String("root", hex.EncodeToString(root[:])),
			zap.Any("validators", validators),
		)

		if !found {
			// TODO error?
			continue
		}

		for _, validator := range validators {
			share := cr.BaseRunner.Share[validator]
			pubKey := share.ValidatorPubKey
			sig, err := cr.BaseRunner.State.ReconstructBeaconSig(cr.BaseRunner.State.PostConsensusContainer, root,
				pubKey[:], validator)
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			// TODO should we return an error here? maybe other sigs are fine?
			if err != nil {
				for _, root := range roots {
					cr.BaseRunner.FallBackAndVerifyEachSignature(cr.BaseRunner.State.PostConsensusContainer, root,
						share.Committee, validator)
				}
				logger.Error("got post-consensus quorum but it has invalid signatures",
					fields.Slot(cr.BaseRunner.State.StartingDuty.DutySlot()),
					zap.Int("validator_index", int(validator)),
					zap.Error(err),
				)
				// TODO: @GalRogozinski
				// return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
			}
			specSig := phase0.BLSSignature{}
			copy(specSig[:], sig)

			if role == types.BNRoleAttester {
				att := beaconObjects[root].(*phase0.Attestation)
				att.Signature = specSig
				// broadcast
				if err := cr.beacon.SubmitAttestation(att); err != nil {
					logger.Error("could not submit to Beacon chain reconstructed attestation",
						fields.Slot(att.Data.Slot),
						zap.Int("validator_index", int(validator)),
						zap.Error(err),
					)
					// TODO: @GalRogozinski
					// return errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
					continue
				}
				// TODO: like AttesterRunner
				logger.Debug("📢 submitted attestation",
					//fields.Slot(att.Data.Slot),
					zap.Int("validator_index", int(validator)), fields.Root(root), zap.String("block_root", hex.EncodeToString(att.Data.BeaconBlockRoot[:])))
			} else if role == types.BNRoleSyncCommittee {
				syncMsg := beaconObjects[root].(*altair.SyncCommitteeMessage)
				syncMsg.Signature = specSig
				// Broadcast
				if err := cr.beacon.SubmitSyncMessage(syncMsg); err != nil {
					logger.Error("could not submit to Beacon chain reconstructed signed sync committee",
						fields.Slot(syncMsg.Slot),
						zap.Int("validator_index", int(validator)),
						zap.Error(err),
					)
					// TODO: @GalRogozinski
					// return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
					continue
				}
				logger.Debug("📢 submitted sync committee message",
					fields.Slot(syncMsg.Slot),
					zap.Int("validator_index", int(validator)),
				)
			}
		}
	}
	cr.BaseRunner.State.Finished = true
	return nil
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
			return types.BNRoleSyncCommittee, []phase0.ValidatorIndex{validator}, true
		}
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
	beaconObjects map[[32]byte]ssz.HashRoot, error error,
) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	beaconObjects = make(map[[32]byte]ssz.HashRoot)
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
		if beaconDuty == nil || beaconDuty.IsStopped {
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
			beaconObjects[root] = unSignedAtt
		case types.BNRoleSyncCommittee:
			// Block root
			blockRoot := types.SSZBytes(beaconVote.BlockRoot[:])
			blockRootSlice := [32]byte{}
			copy(blockRootSlice[:], blockRoot)

			// Sync committee beacon object
			syncMsg := &altair.SyncCommitteeMessage{
				Slot:            slot,
				BeaconBlockRoot: phase0.Root(blockRootSlice),
				ValidatorIndex:  beaconDuty.ValidatorIndex,
			}

			// Root
			domain, err := cr.GetBeaconNode().DomainData(epoch, types.DomainSyncCommittee)
			if err != nil {
				continue
			}
			root, err := types.ComputeETHSigningRoot(blockRoot, domain)
			if err != nil {
				continue
			}

			// Set root and beacon object
			syncCommitteeMap[beaconDuty.ValidatorIndex] = root
			beaconObjects[root] = syncMsg
		}
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

func (cr *CommitteeRunner) executeDuty(logger *zap.Logger, duty types.Duty) error {

	//TODO committeeIndex is 0, is this correct?
	attData, _, err := cr.GetBeaconNode().GetAttestationData(duty.DutySlot(), 0)
	if err != nil {
		return errors.Wrap(err, "failed to get attestation data")
	}

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
