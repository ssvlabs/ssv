package runner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
)

type SyncCommitteeAggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon   specssv.BeaconNode
	network  specssv.Network
	signer   spectypes.KeyManager
	valCheck specqbft.ProposedValueCheckF
	logger   *zap.Logger
}

func NewSyncCommitteeAggregatorRunner(
	beaconNetwork spectypes.BeaconNetwork,
	share *spectypes.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network specssv.Network,
	signer spectypes.KeyManager,
	valCheck specqbft.ProposedValueCheckF,
) Runner {
	logger := logger.With(zap.String("validator", hex.EncodeToString(share.ValidatorPubKey)))
	return &SyncCommitteeAggregatorRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType: spectypes.BNRoleSyncCommitteeContribution,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
			QBFTController: qbftController,
			logger:         logger.With(zap.String("who", "BaseRunner")),
		},

		beacon:   beacon,
		network:  network,
		signer:   signer,
		valCheck: valCheck,
		logger:   logger.With(zap.String("who", "SyncCommitteeAggregatorRunner")),
	}
}

func (r *SyncCommitteeAggregatorRunner) StartNewDuty(duty *spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *SyncCommitteeAggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *SyncCommitteeAggregatorRunner) ProcessPreConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing sync committee selection proof message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	duty := r.GetState().StartingDuty
	input := &spectypes.ConsensusData{
		Duty:                      duty,
		SyncCommitteeContribution: make(map[phase0.BLSSignature]*altair.SyncCommitteeContribution),
	}

	anyIsAggregator := false
	for i, root := range roots {
		// reconstruct selection proof sig
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
		if err != nil {
			return errors.Wrap(err, "could not reconstruct sync committee index root")
		}
		blsSigSelectionProof := phase0.BLSSignature{}
		copy(blsSigSelectionProof[:], sig)

		aggregator, err := r.GetBeaconNode().IsSyncCommitteeAggregator(sig)
		if err != nil {
			return errors.Wrap(err, "could not check if sync committee aggregator")
		}
		if !aggregator {
			continue
		}

		anyIsAggregator = true

		// fetch sync committee contribution
		subnet, err := r.GetBeaconNode().SyncCommitteeSubnetID(r.GetState().StartingDuty.ValidatorSyncCommitteeIndices[i])
		if err != nil {
			return errors.Wrap(err, "could not get sync committee subnet ID")
		}
		contribution, err := r.GetBeaconNode().GetSyncCommitteeContribution(duty.Slot, subnet)
		if err != nil {
			return errors.Wrap(err, "could not get sync committee contribution")
		}

		input.SyncCommitteeContribution[blsSigSelectionProof] = contribution
	}

	if anyIsAggregator {
		if err := r.BaseRunner.decide(r, input); err != nil {
			return errors.Wrap(err, "can't start new duty runner instance for duty")
		}
	} else {
		r.BaseRunner.State.Finished = true
	}

	return nil
}

func (r *SyncCommitteeAggregatorRunner) ProcessConsensus(signedMsg *specqbft.SignedMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	// specific duty sig
	msgs := make([]*specssv.PartialSignatureMessage, 0)
	for proof, c := range decidedValue.SyncCommitteeContribution {
		contribAndProof, _, err := r.generateContributionAndProof(c, proof)
		if err != nil {
			return errors.Wrap(err, "could not generate contribution and proof")
		}

		signed, err := r.BaseRunner.signBeaconObject(r, contribAndProof, decidedValue.Duty.Slot, spectypes.DomainContributionAndProof)
		if err != nil {
			return errors.Wrap(err, "failed to sign aggregate and proof")
		}

		msgs = append(msgs, signed)
	}
	postConsensusMsg := &specssv.PartialSignatureMessages{
		Type:     specssv.PostConsensusPartialSig,
		Messages: msgs,
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   spectypes.NewMsgID(r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}

	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *SyncCommitteeAggregatorRunner) ProcessPostConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
		if err != nil {
			return errors.Wrap(err, "could not reconstruct post consensus signature")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		for proof, contribution := range r.GetState().DecidedValue.SyncCommitteeContribution {
			// match the right contrib and proof root to signed root
			contribAndProof, contribAndProofRoot, err := r.generateContributionAndProof(contribution, proof)
			if err != nil {
				return errors.Wrap(err, "could not generate contribution and proof")
			}
			if !bytes.Equal(root, contribAndProofRoot[:]) {
				continue // not the correct root
			}

			signedContrib, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
			if err != nil {
				return errors.Wrap(err, "could not reconstruct contribution and proof sig")
			}
			blsSignedContribAndProof := phase0.BLSSignature{}
			copy(blsSignedContribAndProof[:], signedContrib)
			signedContribAndProof := &altair.SignedContributionAndProof{
				Message:   contribAndProof,
				Signature: blsSignedContribAndProof,
			}

			if err := r.GetBeaconNode().SubmitSignedContributionAndProof(signedContribAndProof); err != nil {
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed contribution and proof")
			}
			r.logger.Debug("submitted successfully sync committee aggregator!")
			break
		}
	}
	r.GetState().Finished = true
	return nil
}

func (r *SyncCommitteeAggregatorRunner) generateContributionAndProof(contrib *altair.SyncCommitteeContribution, proof phase0.BLSSignature) (*altair.ContributionAndProof, phase0.Root, error) {
	contribAndProof := &altair.ContributionAndProof{
		AggregatorIndex: r.GetState().DecidedValue.Duty.ValidatorIndex,
		Contribution:    contrib,
		SelectionProof:  proof,
	}

	epoch := r.BaseRunner.BeaconNetwork.EstimatedEpochAtSlot(r.GetState().DecidedValue.Duty.Slot)
	dContribAndProof, err := r.GetBeaconNode().DomainData(epoch, spectypes.DomainContributionAndProof)
	if err != nil {
		return nil, phase0.Root{}, errors.Wrap(err, "could not get domain data")
	}
	contribAndProofRoot, err := spectypes.ComputeETHSigningRoot(contribAndProof, dContribAndProof)
	if err != nil {
		return nil, phase0.Root{}, errors.Wrap(err, "could not compute signing root")
	}
	return contribAndProof, contribAndProofRoot, nil
}

func (r *SyncCommitteeAggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	sszIndexes := make([]ssz.HashRoot, 0)
	for _, index := range r.GetState().StartingDuty.ValidatorSyncCommitteeIndices {
		subnet, err := r.GetBeaconNode().SyncCommitteeSubnetID(index)
		if err != nil {
			return nil, spectypes.DomainError, errors.Wrap(err, "could not get sync committee subnet ID")
		}
		data := &altair.SyncAggregatorSelectionData{
			Slot:              r.GetState().StartingDuty.Slot,
			SubcommitteeIndex: subnet,
		}
		sszIndexes = append(sszIndexes, data)
	}
	return sszIndexes, spectypes.DomainSyncCommitteeSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *SyncCommitteeAggregatorRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	ret := make([]ssz.HashRoot, 0)
	for proof, contrib := range r.BaseRunner.State.DecidedValue.SyncCommitteeContribution {
		contribAndProof, _, err := r.generateContributionAndProof(contrib, proof)
		if err != nil {
			return nil, spectypes.DomainError, errors.Wrap(err, "could not generate contribution and proof")
		}
		ret = append(ret, contribAndProof)
	}
	return ret, spectypes.DomainContributionAndProof, nil
}

// executeDuty steps:
// 1) sign a partial contribution proof (for each subcommittee index) and wait for 2f+1 partial sigs from peers
// 2) Reconstruct contribution proofs, check IsSyncCommitteeAggregator and start consensus on duty + contribution data
// 3) Once consensus decides, sign partial contribution data (for each subcommittee) and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedContributionAndProof (for each subcommittee) sig to the BN
func (r *SyncCommitteeAggregatorRunner) executeDuty(duty *spectypes.Duty) error {
	// sign selection proofs
	msgs := specssv.PartialSignatureMessages{
		Type:     specssv.ContributionProofs,
		Messages: []*specssv.PartialSignatureMessage{},
	}
	for _, index := range r.GetState().StartingDuty.ValidatorSyncCommitteeIndices {
		subnet, err := r.GetBeaconNode().SyncCommitteeSubnetID(index)
		if err != nil {
			return errors.Wrap(err, "could not get sync committee subnet ID")
		}
		data := &altair.SyncAggregatorSelectionData{
			Slot:              duty.Slot,
			SubcommitteeIndex: subnet,
		}
		msg, err := r.BaseRunner.signBeaconObject(r, data, duty.Slot, spectypes.DomainSyncCommitteeSelectionProof)
		if err != nil {
			return errors.Wrap(err, "could not sign sync committee selection proof")
		}

		msgs.Messages = append(msgs.Messages, msg)
	}

	// package into signed partial sig
	signature, err := r.GetSigner().SignRoot(msgs, spectypes.PartialSignatureType, r.GetShare().SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for contribution proofs")
	}
	signedPartialMsg := &specssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: signature,
		Signer:    r.GetShare().OperatorID,
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode contribution proofs pre-consensus signature msg")
	}
	msgToBroadcast := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   spectypes.NewMsgID(r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}
	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial contribution proof sig")
	}
	return nil
}

func (r *SyncCommitteeAggregatorRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *SyncCommitteeAggregatorRunner) GetNetwork() specssv.Network {
	return r.network
}

func (r *SyncCommitteeAggregatorRunner) GetBeaconNode() specssv.BeaconNode {
	return r.beacon
}

func (r *SyncCommitteeAggregatorRunner) GetShare() *spectypes.Share {
	return r.BaseRunner.Share
}

func (r *SyncCommitteeAggregatorRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *SyncCommitteeAggregatorRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *SyncCommitteeAggregatorRunner) GetSigner() spectypes.KeyManager {
	return r.signer
}

// Encode returns the encoded struct in bytes or error
func (r *SyncCommitteeAggregatorRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *SyncCommitteeAggregatorRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *SyncCommitteeAggregatorRunner) GetRoot() ([]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}
