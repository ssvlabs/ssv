package runner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"

	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/metrics"
)

type ProposerRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner spectypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

func NewProposerRunner(
	beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner spectypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &ProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleProposer,
			BeaconNetwork:      beaconNetwork,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		valCheck:       valCheck,
		operatorSigner: operatorSigner,

		metrics: metrics.NewConsensusMetrics(spectypes.BNRoleProposer),
	}
}

func (r *ProposerRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty, quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *ProposerRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *ProposerRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing randao message")
	}

	duty := r.GetState().StartingDuty.(*spectypes.BeaconDuty)

	logger = logger.With(fields.Slot(duty.DutySlot()))
	logger.Debug("🧩 got partial RANDAO signatures",
		zap.Uint64("signer", signedMsg.Messages[0].Signer)) // TODO: more than one? check index arr boundries

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	r.metrics.EndPreConsensus()

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	// randao is relevant only for block proposals, no need to check type
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	logger.Debug("🧩 reconstructed partial RANDAO signatures",
		zap.Uint64s("signers", getPreConsensusSigners(r.GetState(), root)))

	start := time.Now()

	obj, ver, err := r.GetBeaconNode().GetBeaconBlock(duty.Slot, r.GetShare().Graffiti, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to get beacon block")
	}

	took := time.Since(start)
	// Log essentials about the retrieved block.
	blockSummary, summarizeErr := summarizeBlock(obj)
	logger.Info("🧊 got beacon block proposal",
		zap.String("block_hash", blockSummary.Hash.String()),
		zap.Bool("blinded", blockSummary.Blinded),
		zap.Duration("took", took),
		zap.NamedError("summarize_err", summarizeErr))

	byts, err := obj.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal beacon block")
	}

	input := &spectypes.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	inputBytes, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode ConsensusData")
	}

	r.metrics.StartConsensus()
	if err := r.BaseRunner.decide(logger, r, duty.Slot, inputBytes); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *ProposerRunner) ProcessConsensus(logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}
	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.metrics.EndConsensus()
	r.metrics.StartPostConsensus()

	// specific duty sig
	var blkToSign ssz.HashRoot

	cd := decidedValue.(*spectypes.ConsensusData)

	if r.decidedBlindedBlock() {
		_, blkToSign, err = cd.GetBlindedBlockData()
		if err != nil {
			return errors.Wrap(err, "could not get blinded block data")
		}
	} else {
		_, blkToSign, err = cd.GetBlockData()
		if err != nil {
			return errors.Wrap(err, "could not get block data")
		}
	}

	msg, err := r.BaseRunner.signBeaconObject(
		r,
		r.BaseRunner.State.StartingDuty.(*spectypes.BeaconDuty),
		blkToSign,
		cd.Duty.Slot,
		spectypes.DomainProposer,
	)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	msgToBroadcast, err := spectypes.PartialSignatureMessagesToSignedSSVMessage(postConsensusMsg, msgID, r.operatorSigner)
	if err != nil {
		return errors.Wrap(err, "could not sign post-consensus partial signature message")
	}

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *ProposerRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}
	if !quorum {
		return nil
	}

	r.metrics.EndPostConsensus()

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		blockSubmissionEnd := r.metrics.StartBeaconSubmission()

		consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
		if err != nil {
			return errors.Wrap(err, "could not create consensus data")
		}

		start := time.Now()
		var blk any
		if r.decidedBlindedBlock() {
			vBlindedBlk, _, err := consensusData.GetBlindedBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get blinded block")
			}
			blk = vBlindedBlk

			if err := r.GetBeaconNode().SubmitBlindedBeaconBlock(vBlindedBlk, specSig); err != nil {
				r.metrics.RoleSubmissionFailed()

				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed blinded Beacon block")
			}
		} else {
			vBlk, _, err := consensusData.GetBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get block")
			}
			blk = vBlk

			if err := r.GetBeaconNode().SubmitBeaconBlock(vBlk, specSig); err != nil {
				r.metrics.RoleSubmissionFailed()

				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed Beacon block")
			}
		}

		blockSubmissionEnd()
		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		blockSummary, summarizeErr := summarizeBlock(blk)
		logger.Info("✅ successfully submitted block proposal",
			fields.Slot(consensusData.Duty.Slot),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.GetState().RunningInstance.State.Round),
			zap.String("block_hash", blockSummary.Hash.String()),
			zap.Bool("blinded", blockSummary.Blinded),
			zap.Duration("took", time.Since(start)),
			zap.NamedError("summarize_err", summarizeErr))
	}
	r.GetState().Finished = true
	return nil
}

// decidedBlindedBlock returns true if decided value has a blinded block, false if regular block
// WARNING!! should be called after decided only
func (r *ProposerRunner) decidedBlindedBlock() bool {
	consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
	if err != nil {
		return false
	}
	_, _, err = consensusData.GetBlindedBlockData()
	return err == nil
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	epoch := r.BaseRunner.BeaconNetwork.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot())
	return []ssz.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {

	consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not create consensus data")
	}

	if r.decidedBlindedBlock() {
		_, data, err := consensusData.GetBlindedBlockData()
		if err != nil {
			return nil, phase0.DomainType{}, errors.Wrap(err, "could not get blinded block data")
		}
		return []ssz.HashRoot{data}, spectypes.DomainProposer, nil
	}

	_, data, err := consensusData.GetBlockData()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get block data")
	}
	return []ssz.HashRoot{data}, spectypes.DomainProposer, nil
}

// executeDuty steps:
// 1) sign a partial randao sig and wait for 2f+1 partial sigs from peers
// 2) reconstruct randao and send GetBeaconBlock to BN
// 3) start consensus on duty + block data
// 4) Once consensus decides, sign partial block and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid block sig to the BN
func (r *ProposerRunner) executeDuty(logger *zap.Logger, duty spectypes.Duty) error {
	r.metrics.StartDutyFullFlow()
	r.metrics.StartPreConsensus()

	// sign partial randao
	epoch := r.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(duty.DutySlot())
	msg, err := r.BaseRunner.signBeaconObject(r, duty.(*spectypes.BeaconDuty), spectypes.SSZUint64(epoch), duty.DutySlot(), spectypes.DomainRandao)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	msgToBroadcast, err := spectypes.PartialSignatureMessagesToSignedSSVMessage(msgs, msgID, r.operatorSigner)
	if err != nil {
		return errors.Wrap(err, "could not sign pre-consensus partial signature message")
	}
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial randao sig")
	}

	logger.Debug("🔏 signed & broadcasted partial RANDAO signature")

	return nil
}

func (r *ProposerRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *ProposerRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *ProposerRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ProposerRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *ProposerRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *ProposerRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *ProposerRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}

func (r *ProposerRunner) GetOperatorSigner() spectypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *ProposerRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ProposerRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *ProposerRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// blockSummary contains essentials about a block. Useful for logging.
type blockSummary struct {
	Hash    phase0.Hash32
	Blinded bool
	Version spec.DataVersion
}

// summarizeBlock returns a blockSummary for the given block.
func summarizeBlock(block any) (summary blockSummary, err error) {
	if block == nil {
		return summary, fmt.Errorf("block is nil")
	}
	switch b := block.(type) {
	case *api.VersionedProposal:
		if b.Blinded {
			switch b.Version {
			case spec.DataVersionCapella:
				return summarizeBlock(b.CapellaBlinded)
			case spec.DataVersionDeneb:
				return summarizeBlock(b.DenebBlinded)
			default:
				return summary, fmt.Errorf("unsupported blinded block version %d", b.Version)
			}
		}
		switch b.Version {
		case spec.DataVersionCapella:
			return summarizeBlock(b.Capella)
		case spec.DataVersionDeneb:
			if b.Deneb == nil {
				return summary, fmt.Errorf("deneb block contents is nil")
			}
			return summarizeBlock(b.Deneb.Block)
		default:
			return summary, fmt.Errorf("unsupported block version %d", b.Version)
		}

	case *capella.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return summary, fmt.Errorf("block, body or execution payload is nil")
		}
		summary.Hash = b.Body.ExecutionPayload.BlockHash
		summary.Version = spec.DataVersionCapella

	case *deneb.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return summary, fmt.Errorf("block, body or execution payload is nil")
		}
		summary.Hash = b.Body.ExecutionPayload.BlockHash
		summary.Version = spec.DataVersionDeneb

	case *apiv1deneb.BlockContents:
		return summarizeBlock(b.Block)

	case *apiv1capella.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return summary, fmt.Errorf("block, body or execution payload header is nil")
		}
		summary.Hash = b.Body.ExecutionPayloadHeader.BlockHash
		summary.Blinded = true
		summary.Version = spec.DataVersionCapella

	case *apiv1deneb.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return summary, fmt.Errorf("block, body or execution payload header is nil")
		}
		summary.Hash = b.Body.ExecutionPayloadHeader.BlockHash
		summary.Blinded = true
		summary.Version = spec.DataVersionDeneb
	}
	return
}
