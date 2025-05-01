package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type ProposerRunner struct {
	BaseRunner *BaseRunner

	beacon              beacon.BeaconNode
	network             specqbft.Network
	signer              ekm.BeaconSigner
	operatorSigner      ssvtypes.OperatorSigner
	doppelgangerHandler DoppelgangerProvider
	valCheck            specqbft.ProposedValueCheckF
	measurements        measurementsStore
	graffiti            []byte
}

func NewProposerRunner(
	domainType spectypes.DomainType,
	beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	doppelgangerHandler DoppelgangerProvider,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
	graffiti []byte,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &ProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleProposer,
			DomainType:         domainType,
			BeaconNetwork:      beaconNetwork,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:              beacon,
		network:             network,
		signer:              signer,
		operatorSigner:      operatorSigner,
		doppelgangerHandler: doppelgangerHandler,
		valCheck:            valCheck,
		graffiti:            graffiti,
		measurements:        NewMeasurementsStore(),
	}, nil
}

func (r *ProposerRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *ProposerRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *ProposerRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing randao message")
	}

	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)

	logger = logger.With(fields.Slot(duty.DutySlot()))
	logger.Debug("🧩 got partial RANDAO signatures",
		zap.Uint64("signer", signedMsg.Messages[0].Signer)) // TODO: more than one? check index arr boundries

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleProposer)

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
		zap.Uint64s("signers", getPreConsensusSigners(r.GetState(), root)),
		fields.PreConsensusTime(r.measurements.PreConsensusTime()))

	start := time.Now()
	duty = r.GetState().StartingDuty.(*spectypes.ValidatorDuty)
	obj, ver, err := r.GetBeaconNode().GetBeaconBlock(duty.Slot, r.graffiti, fullSig)
	if err != nil {
		logger.Error("❌ failed to get beacon block",
			fields.PreConsensusTime(r.measurements.PreConsensusTime()),
			fields.BlockTime(time.Since(start)),
			zap.Error(err))
		return errors.Wrap(err, "failed to get beacon block")
	}

	// Log essentials about the retrieved block.
	blockSummary, summarizeErr := summarizeBlock(obj)
	logger.Info("🧊 got beacon block proposal",
		zap.String("block_hash", blockSummary.Hash.String()),
		zap.Bool("blinded", blockSummary.Blinded),
		zap.Duration("took", time.Since(start)),
		zap.NamedError("summarize_err", summarizeErr))

	byts, err := obj.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal beacon block")
	}
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	r.measurements.StartConsensus()

	if err := r.BaseRunner.decide(ctx, logger, r, duty.Slot, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *ProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r, signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}
	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleProposer)

	r.measurements.StartPostConsensus()

	// specific duty sig
	var blkToSign ssz.HashRoot

	cd := decidedValue.(*spectypes.ValidatorConsensusData)

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

	duty := r.BaseRunner.State.StartingDuty.(*spectypes.ValidatorDuty)
	if !r.doppelgangerHandler.CanSign(duty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(duty.ValidatorIndex))
		return nil
	}

	msg, err := r.BaseRunner.signBeaconObject(ctx, r, duty, blkToSign, cd.Duty.Slot, spectypes.DomainProposer)
	if err != nil {
		return errors.Wrap(err, "failed signing block")
	}
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := postConsensusMsg.Encode()
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

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *ProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}
	if !quorum {
		return nil
	}

	var successfullySubmittedProposals uint8
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

		r.measurements.EndPostConsensus()
		recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleProposer)

		logger.Debug("🧩 reconstructed partial post consensus signatures proposer",
			zap.Uint64s("signers", getPostConsensusProposerSigners(r.GetState(), root)),
			fields.PostConsensusTime(r.measurements.PostConsensusTime()),
			fields.Round(r.GetState().RunningInstance.State.Round))

		r.doppelgangerHandler.ReportQuorum(r.GetShare().ValidatorIndex)

		validatorConsensusData := &spectypes.ValidatorConsensusData{}
		err = validatorConsensusData.Decode(r.GetState().DecidedValue)
		if err != nil {
			return errors.Wrap(err, "could not create consensus data")
		}

		start := time.Now()

		logger = logger.With(
			fields.PreConsensusTime(r.measurements.PreConsensusTime()),
			fields.ConsensusTime(r.measurements.ConsensusTime()),
			fields.PostConsensusTime(r.measurements.PostConsensusTime()),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.GetState().RunningInstance.State.Round),
			zap.Bool("blinded", r.decidedBlindedBlock()),
		)
		var (
			blockSummary blockSummary
			summarizeErr error
		)
		if r.decidedBlindedBlock() {
			vBlindedBlk, _, err := validatorConsensusData.GetBlindedBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get blinded block")
			}
			blockSummary, summarizeErr = summarizeBlock(vBlindedBlk)
			logger = logger.With(
				zap.String("block_hash", blockSummary.Hash.String()),
				zap.NamedError("summarize_err", summarizeErr),
			)

			if err := r.GetBeaconNode().SubmitBlindedBeaconBlock(vBlindedBlk, specSig); err != nil {
				recordFailedSubmission(ctx, spectypes.BNRoleProposer)
				logger.Error("❌ could not submit blinded Beacon block",
					fields.SubmissionTime(time.Since(start)),
					zap.Error(err))
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed blinded Beacon block")
			}
		} else {
			vBlk, _, err := validatorConsensusData.GetBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get block")
			}
			blockSummary, summarizeErr = summarizeBlock(vBlk)
			logger = logger.With(
				zap.String("block_hash", blockSummary.Hash.String()),
				zap.NamedError("summarize_err", summarizeErr),
			)

			if err := r.GetBeaconNode().SubmitBeaconBlock(vBlk, specSig); err != nil {
				recordFailedSubmission(ctx, spectypes.BNRoleProposer)
				logger.Error("❌ could not submit Beacon block",
					fields.SubmissionTime(time.Since(start)),
					zap.Error(err))
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed Beacon block")
			}
		}

		successfullySubmittedProposals++
		logger.Info("✅ successfully submitted block proposal",
			fields.Slot(validatorConsensusData.Duty.Slot),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.GetState().RunningInstance.State.Round),
			zap.String("block_hash", blockSummary.Hash.String()),
			zap.Bool("blinded", blockSummary.Blinded),
			zap.Duration("took", time.Since(start)),
			zap.NamedError("summarize_err", summarizeErr),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()))
	}

	r.GetState().Finished = true

	r.measurements.EndDutyFlow()

	recordDutyDuration(ctx, r.measurements.DutyDurationTime(), spectypes.BNRoleProposer, r.GetState().RunningInstance.State.Round)
	recordSuccessfulSubmission(ctx,
		uint32(successfullySubmittedProposals),
		r.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot()),
		spectypes.BNRoleProposer)

	return nil
}

// decidedBlindedBlock returns true if decided value has a blinded block, false if regular block
// WARNING!! should be called after decided only
func (r *ProposerRunner) decidedBlindedBlock() bool {
	validatorConsensusData := &spectypes.ValidatorConsensusData{}
	err := validatorConsensusData.Decode(r.GetState().DecidedValue)
	if err != nil {
		return false
	}
	_, _, err = validatorConsensusData.GetBlindedBlockData()
	return err == nil
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	epoch := r.BaseRunner.BeaconNetwork.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot())
	return []ssz.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	validatorConsensusData := &spectypes.ValidatorConsensusData{}
	err := validatorConsensusData.Decode(r.GetState().DecidedValue)
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not create consensus data")
	}

	if r.decidedBlindedBlock() {
		_, data, err := validatorConsensusData.GetBlindedBlockData()
		if err != nil {
			return nil, phase0.DomainType{}, errors.Wrap(err, "could not get blinded block data")
		}
		return []ssz.HashRoot{data}, spectypes.DomainProposer, nil
	}

	_, data, err := validatorConsensusData.GetBlockData()
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
func (r *ProposerRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	r.measurements.StartDutyFlow()
	r.measurements.StartPreConsensus()

	proposerDuty := duty.(*spectypes.ValidatorDuty)
	if !r.doppelgangerHandler.CanSign(proposerDuty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(proposerDuty.ValidatorIndex))
		return nil
	}

	// sign partial randao
	epoch := r.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(duty.DutySlot())
	msg, err := r.BaseRunner.signBeaconObject(
		ctx,
		r,
		proposerDuty,
		spectypes.SSZUint64(epoch),
		duty.DutySlot(),
		spectypes.DomainRandao,
	)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
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

func (r *ProposerRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *ProposerRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
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
		return [32]byte{}, errors.Wrap(err, "could not encode ProposerRunner")
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
			case spec.DataVersionElectra:
				return summarizeBlock(b.ElectraBlinded)
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
		case spec.DataVersionElectra:
			if b.Electra == nil {
				return summary, fmt.Errorf("electra block contents is nil")
			}
			return summarizeBlock(b.Electra.Block)
		default:
			return summary, fmt.Errorf("unsupported block version %d", b.Version)
		}

	case *api.VersionedBlindedProposal:
		switch b.Version {
		case spec.DataVersionCapella:
			return summarizeBlock(b.Capella)
		case spec.DataVersionDeneb:
			return summarizeBlock(b.Deneb)
		case spec.DataVersionElectra:
			return summarizeBlock(b.Electra)
		default:
			return summary, fmt.Errorf("unsupported blinded block version %d", b.Version)
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

	case *electra.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return summary, fmt.Errorf("block, body or execution payload is nil")
		}
		summary.Hash = b.Body.ExecutionPayload.BlockHash
		summary.Version = spec.DataVersionElectra

	case *apiv1electra.BlockContents:
		return summarizeBlock(b.Block)

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

	case *apiv1electra.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return summary, fmt.Errorf("block, body or execution payload header is nil")
		}
		summary.Hash = b.Body.ExecutionPayloadHeader.BlockHash
		summary.Blinded = true
		summary.Version = spec.DataVersionElectra
	}

	return
}
