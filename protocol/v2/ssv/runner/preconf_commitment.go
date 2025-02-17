package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type PreconfCommitmentRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	gasLimit uint64
}

func NewPreconfCommitmentRunner(
	domainType spectypes.DomainType,
	beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	gasLimit uint64,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &PreconfCommitmentRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RolePreconfCommitment,
			DomainType:     domainType,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		gasLimit:       gasLimit,
	}, nil
}

func (r *PreconfCommitmentRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewNonBeaconDuty(ctx, logger, r, duty.(*spectypes.ValidatorDuty), quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *PreconfCommitmentRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *PreconfCommitmentRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	// TODO - provide appropriate implementation

	//quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	//if err != nil {
	//	return errors.Wrap(err, "failed processing validator registration message")
	//}
	//
	//// TODO: (Alan) revert
	//logger.Debug("got partial sig",
	//	zap.Uint64("signer", signedMsg.Messages[0].Signer),
	//	zap.Bool("quorum", quorum))
	//
	//// quorum returns true only once (first time quorum achieved)
	//if !quorum {
	//	return nil
	//}
	//
	//// only 1 root, verified in basePreConsensusMsgProcessing
	//root := roots[0]
	//fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	//if err != nil {
	//	// If the reconstructed signature verification failed, fall back to verifying each partial signature
	//	r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee,
	//		r.GetShare().ValidatorIndex)
	//	return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	//}
	//specSig := phase0.BLSSignature{}
	//copy(specSig[:], fullSig)
	//
	//share := r.GetShare()
	//if share == nil {
	//	return errors.New("no share to get validator public key")
	//}
	//
	//registration, err := r.calculatePreconfCommitment(r.BaseRunner.State.StartingDuty.DutySlot())
	//if err != nil {
	//	return errors.Wrap(err, "could not calculate validator registration")
	//}
	//
	//signed := &api.VersionedSignedPreconfCommitment{
	//	Version: spec.BuilderVersionV1,
	//	V1: &v1.SignedPreconfCommitment{
	//		Message:   registration,
	//		Signature: specSig,
	//	},
	//}
	//
	//if err := r.beacon.SubmitPreconfCommitment(signed); err != nil {
	//	return errors.Wrap(err, "could not submit validator registration")
	//}
	//
	//logger.Debug("validator registration submitted successfully",
	//	fields.FeeRecipient(share.FeeRecipientAddress[:]),
	//	zap.String("signature", hex.EncodeToString(specSig[:])))
	//
	//r.GetState().Finished = true
	return nil
}

func (r *PreconfCommitmentRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for validator registration")
}

func (r *PreconfCommitmentRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for validator registration")
}

func (r *PreconfCommitmentRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	// TODO - implement
	// compare hash-root that comes with pre-consensus message against what this runner expects
	// based on the data it has pulled on its own
	// note, currently with Bolt there is no way for us to fetch data about preconf(s) independently:
	// https://github.com/chainbound/bolt/issues/772
	return TODO, TODO, TODO
	//if r.BaseRunner.State == nil || r.BaseRunner.State.StartingDuty == nil {
	//	return nil, spectypes.DomainError, errors.New("no running duty to compute preconsensus roots and domain")
	//}
	//vr, err := r.calculatePreconfCommitment(r.BaseRunner.State.StartingDuty.DutySlot())
	//if err != nil {
	//	return nil, spectypes.DomainError, errors.Wrap(err, "could not calculate validator registration")
	//}
	//return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *PreconfCommitmentRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for validator registration")
}

func (r *PreconfCommitmentRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	// TODO - adjust this for preconfs

	//vr, err := r.calculatePreconfCommitment(duty.DutySlot())
	//if err != nil {
	//	return errors.Wrap(err, "could not calculate validator registration")
	//}
	//
	//// sign partial randao
	//msg, err := r.BaseRunner.signBeaconObject(r, duty.(*spectypes.ValidatorDuty), vr, duty.DutySlot(),
	//	spectypes.DomainApplicationBuilder)
	//if err != nil {
	//	return errors.Wrap(err, "could not sign validator registration")
	//}
	//msgs := &spectypes.PartialSignatureMessages{
	//	Type:     spectypes.PreconfCommitmentPartialSig,
	//	Slot:     duty.DutySlot(),
	//	Messages: []*spectypes.PartialSignatureMessage{msg},
	//}
	//
	//msgID := spectypes.NewMsgID(r.BaseRunner.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	//encodedMsg, err := msgs.Encode()
	//if err != nil {
	//	return err
	//}
	//
	//ssvMsg := &spectypes.SSVMessage{
	//	MsgType: spectypes.SSVPartialSignatureMsgType,
	//	MsgID:   msgID,
	//	Data:    encodedMsg,
	//}
	//
	//sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	//if err != nil {
	//	return errors.Wrap(err, "could not sign SSVMessage")
	//}
	//msgToBroadcast := &spectypes.SignedSSVMessage{
	//	Signatures:  [][]byte{sig},
	//	OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
	//	SSVMessage:  ssvMsg,
	//}
	//
	//logger.Debug(
	//	"broadcasting validator registration partial sig",
	//	fields.Slot(duty.DutySlot()),
	//	zap.Any("validator_registration", vr),
	//)
	//
	//if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
	//	return errors.Wrap(err, "can't broadcast partial randao sig")
	//}

	return nil
}

func (r *PreconfCommitmentRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *PreconfCommitmentRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *PreconfCommitmentRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *PreconfCommitmentRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *PreconfCommitmentRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *PreconfCommitmentRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *PreconfCommitmentRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}
func (r *PreconfCommitmentRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *PreconfCommitmentRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *PreconfCommitmentRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *PreconfCommitmentRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode PreconfCommitmentRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
