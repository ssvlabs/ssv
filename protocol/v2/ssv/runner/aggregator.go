package runner

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
)

type AggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon   ssv.BeaconNode
	network  ssv.Network
	signer   types.KeyManager
	valCheck qbft.ProposedValueCheckF
}

func NewAggregatorRunner(beaconNetwork types.BeaconNetwork, share *types.Share, qbftController *controller.Controller, beacon ssv.BeaconNode, network ssv.Network, signer types.KeyManager, valCheck qbft.ProposedValueCheckF) Runner {
	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType: types.BNRoleAggregator,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
			QBFTController: qbftController,
		},

		beacon:   beacon,
		network:  network,
		signer:   signer,
		valCheck: valCheck,
	}
}

func (r *AggregatorRunner) StartNewDuty(duty *types.Duty) error {
	return r.BaseRunner.baseStartNewDuty(r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.HashRunningDuty()
}

func (r *AggregatorRunner) ProcessPreConsensus(signedMsg *ssv.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// only 1 root, verified by basePreConsensusMsgProcessing
	root := roots[0]
	// reconstruct selection proof sig
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
	if err != nil {
		return errors.Wrap(err, "could not reconstruct selection proof sig")
	}

	duty := r.GetState().StartingDuty

	// TODO waitToSlotTwoThirds

	// get block data
	res, err := r.GetBeaconNode().SubmitAggregateSelectionProof(duty.Slot, duty.CommitteeIndex, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to submit aggregate and proof")
	}

	input := &types.ConsensusData{
		Duty:              duty,
		AggregateAndProof: res,
	}

	if err := r.BaseRunner.decide(r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(signedMsg *qbft.SignedMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(r, decidedValue.AggregateAndProof, decidedValue.Duty.Slot, types.DomainAggregateAndProof)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &ssv.PartialSignatureMessages{
		Type:     ssv.PostConsensusPartialSig,
		Messages: []*ssv.PartialSignatureMessage{msg},
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID:   types.NewMsgID(r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}

	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *AggregatorRunner) ProcessPostConsensus(signedMsg *ssv.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(signedMsg)
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

		msg := &phase0.SignedAggregateAndProof{
			Message:   r.GetState().DecidedValue.AggregateAndProof,
			Signature: specSig,
		}
		if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(msg); err != nil {
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed aggregate")
		}
	}
	r.GetState().Finished = true
	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{types.SSZUint64(r.GetState().StartingDuty.Slot)}, types.DomainSelectionProof, nil
}

// executeDuty steps:
// 1) sign a partial selection proof and wait for 2f+1 partial sigs from peers
// 2) reconstruct selection proof and send SubmitAggregateSelectionProof to BN
// 3) start consensus on duty + aggregation data
// 4) Once consensus decides, sign partial aggregation data and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedAggregateSubmitRequest sig to the BN
func (r *AggregatorRunner) executeDuty(duty *types.Duty) error {
	// sign selection proof
	msg, err := r.BaseRunner.signBeaconObject(r, types.SSZUint64(duty.Slot), duty.Slot, types.DomainSelectionProof)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := ssv.PartialSignatureMessages{
		Type:     ssv.SelectionProofPartialSig,
		Messages: []*ssv.PartialSignatureMessage{msg},
	}

	// sign msg
	signature, err := r.GetSigner().SignRoot(msg, types.PartialSignatureType, r.GetShare().SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for selection proof")
	}
	signedPartialMsg := &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: signature,
		Signer:    r.GetShare().OperatorID,
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode selection proof pre-consensus signature msg")
	}
	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID:   types.NewMsgID(r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}
	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial selection proof sig")
	}
	return nil
}

func (r *AggregatorRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AggregatorRunner) GetNetwork() ssv.Network {
	return r.network
}

func (r *AggregatorRunner) GetBeaconNode() ssv.BeaconNode {
	return r.beacon
}

func (r *AggregatorRunner) GetShare() *types.Share {
	return r.BaseRunner.Share
}

func (r *AggregatorRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *AggregatorRunner) Init() error {
	return r.BaseRunner.Init()
}

func (r *AggregatorRunner) GetValCheckF() qbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AggregatorRunner) GetSigner() types.KeyManager {
	return r.signer
}

// Encode returns the encoded struct in bytes or error
func (r *AggregatorRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *AggregatorRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *AggregatorRunner) GetRoot() ([]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}
