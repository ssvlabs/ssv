package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/metrics"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type ValidatorRegistrationRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

func NewValidatorRegistrationRunner(
	domainTypeProvider networkconfig.DomainTypeProvider,
	beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
) Runner {
	return &ValidatorRegistrationRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleValidatorRegistration,
			DomainTypeProvider: domainTypeProvider,
			BeaconNetwork:      beaconNetwork,
			Share:              share,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,

		metrics: metrics.NewConsensusMetrics(spectypes.RoleValidatorRegistration),
	}
}

func (r *ValidatorRegistrationRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewNonBeaconDuty(logger, r, duty.(*spectypes.ValidatorDuty), quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *ValidatorRegistrationRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *ValidatorRegistrationRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing validator registration message")
	}

	// TODO: (Alan) revert
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
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], fullSig)

	share := r.GetShare()
	if share == nil {
		return errors.New("no share to get validator public key")
	}

	if err := r.beacon.SubmitValidatorRegistration(share.ValidatorPubKey[:],
		share.FeeRecipientAddress, specSig); err != nil {
		return errors.Wrap(err, "could not submit validator registration")
	}

	logger.Debug("validator registration submitted successfully",
		fields.FeeRecipient(share.FeeRecipientAddress[:]),
		zap.String("signature", hex.EncodeToString(specSig[:])))

	r.GetState().Finished = true
	return nil
}

func (r *ValidatorRegistrationRunner) ProcessConsensus(logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for validator registration")
}

func (r *ValidatorRegistrationRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.BaseRunner.State == nil || r.BaseRunner.State.StartingDuty == nil {
		return nil, spectypes.DomainError, errors.New("no running duty to compute preconsensus roots and domain")
	}
	vr, err := r.calculateValidatorRegistration(r.BaseRunner.State.StartingDuty)
	if err != nil {
		return nil, spectypes.DomainError, errors.Wrap(err, "could not calculate validator registration")
	}
	return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ValidatorRegistrationRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for validator registration")
}

func (r *ValidatorRegistrationRunner) executeDuty(logger *zap.Logger, duty spectypes.Duty) error {
	vr, err := r.calculateValidatorRegistration(duty)
	if err != nil {
		return errors.Wrap(err, "could not calculate validator registration")
	}

	// sign partial randao
	msg, err := r.BaseRunner.signBeaconObject(r, duty.(*spectypes.ValidatorDuty), vr, duty.DutySlot(),
		spectypes.DomainApplicationBuilder)
	if err != nil {
		return errors.Wrap(err, "could not sign validator registration")
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ValidatorRegistrationPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.DomainTypeProvider.DomainType(), r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return err
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign SSVMessage")
	}
	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	logger.Debug(
		"broadcasting validator registration partial sig",
		fields.Slot(duty.DutySlot()),
		zap.Any("validator_registration", vr),
	)

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial randao sig")
	}
	return nil
}

func (r *ValidatorRegistrationRunner) calculateValidatorRegistration(duty spectypes.Duty) (*v1.ValidatorRegistration, error) {
	share := r.GetShare()
	if share == nil {
		return nil, errors.New("no share to get validator public key")
	}

	pk := phase0.BLSPubKey{}
	copy(pk[:], share.ValidatorPubKey[:])

	epoch := r.BaseRunner.BeaconNetwork.EstimatedEpochAtSlot(duty.DutySlot())

	return &v1.ValidatorRegistration{
		FeeRecipient: share.FeeRecipientAddress,
		GasLimit:     spectypes.DefaultGasLimit,
		Timestamp:    r.BaseRunner.BeaconNetwork.EpochStartTime(epoch),
		Pubkey:       pk,
	}, nil
}

func (r *ValidatorRegistrationRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *ValidatorRegistrationRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *ValidatorRegistrationRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ValidatorRegistrationRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *ValidatorRegistrationRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *ValidatorRegistrationRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *ValidatorRegistrationRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}
func (r *ValidatorRegistrationRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *ValidatorRegistrationRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ValidatorRegistrationRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *ValidatorRegistrationRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode ValidatorRegistrationRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
