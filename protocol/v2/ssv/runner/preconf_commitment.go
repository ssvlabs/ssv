package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type PreconfCommitmentRunner struct {
	childRunners map[[32]byte]*BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
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
	// TODO - implement this for preconfs, this is where we process messages from other
	// operators and once we got quorum we should reconstruct full-validator-signature and
	// send it to whoever was asking for it

	logger = logger.With(zap.String("preconf_commitment_runner", "process pre-consensus message"))

	root := signedMsg.Messages[0].SigningRoot
	if root == [32]byte{} {
		return errors.New("pre-consensus message has empty root")
	}

	childRunner, ok := r.childRunners[root]
	if !ok {

	}

	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing validator registration message")
	}

	logger.Debug("got partial sig",
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Bool("quorum", quorum))

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee,
			r.GetShare().ValidatorIndex)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], fullSig)

	share := r.GetShare()
	if share == nil {
		return errors.New("no share to get validator public key")
	}

	registration, err := r.calculatePreconfCommitment(r.BaseRunner.State.StartingDuty.DutySlot())
	if err != nil {
		return errors.Wrap(err, "could not calculate validator registration")
	}

	signed := &api.VersionedSignedPreconfCommitment{
		Version: spec.BuilderVersionV1,
		V1: &v1.SignedPreconfCommitment{
			Message:   registration,
			Signature: specSig,
		},
	}

	if err := r.beacon.SubmitPreconfCommitment(signed); err != nil {
		return errors.Wrap(err, "could not submit validator registration")
	}

	logger.Debug("validator registration submitted successfully",
		fields.FeeRecipient(share.FeeRecipientAddress[:]),
		zap.String("signature", hex.EncodeToString(specSig[:])))

	r.GetState().Finished = true
	return nil
}

func (r *PreconfCommitmentRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for validator registration")
}

func (r *PreconfCommitmentRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for validator registration")
}

func (r *PreconfCommitmentRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	// TODO - implement this for preconfs
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
	// TODO - implement this for preconfs, this is the entry-point for OUR operator

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

func (r *PreconfCommitmentRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *PreconfCommitmentRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *PreconfCommitmentRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *PreconfCommitmentRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *PreconfCommitmentRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *PreconfCommitmentRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *PreconfCommitmentRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *PreconfCommitmentRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
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
	return nil
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
