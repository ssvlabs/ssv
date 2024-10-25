package runner

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/runner/metrics"
	"go.uber.org/zap"
)

type AggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon   genesisspecssv.BeaconNode
	network  genesisspecssv.Network
	signer   genesisspectypes.KeyManager
	valCheck genesisspecqbft.ProposedValueCheckF
	metrics  metrics.ConsensusMetrics
}

var _ Runner = &AggregatorRunner{}

func NewAggregatorRunner(
	domainType spectypes.DomainType,
	beaconNetwork genesisspectypes.BeaconNetwork,
	share *genesisspectypes.Share,
	qbftController *controller.Controller,
	beacon genesisspecssv.BeaconNode,
	network genesisspecssv.Network,
	signer genesisspectypes.KeyManager,
	valCheck genesisspecqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType:     genesisspectypes.BNRoleAggregator,
			DomainType:         domainType,
			BeaconNetwork:      beaconNetwork,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},
		beacon:   beacon,
		network:  network,
		signer:   signer,
		valCheck: valCheck,
		metrics:  metrics.NewConsensusMetrics(genesisspectypes.BNRoleAggregator),
	}
}

func (r *AggregatorRunner) StartNewDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AggregatorRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}
	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	r.metrics.EndPreConsensus()

	// only 1 root, verified by basePreConsensusMsgProcessing
	root := roots[0]
	// reconstruct selection proof sig
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	duty := r.GetState().StartingDuty

	logger.Debug("🧩 got partial signature quorum",
		zap.Any("signer", signedMsg.Signer),
		fields.Slot(duty.Slot),
	)

	// this is the earliest in aggregator runner flow where we get to know whether we are meant
	// to perform this aggregation duty or not
	ok, err := isAggregator(duty.CommitteeLength, fullSig)
	if err != nil {
		return fmt.Errorf("check if validator is an aggregator: %w", err)
	}
	if !ok {
		logger.Debug("aggregation duty won't be needed from this validator for this slot",
			zap.Any("signer", signedMsg.Signer),
			fields.Slot(duty.Slot),
		)
		return nil
	}

	r.metrics.PauseDutyFullFlow()
	// get block data
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to submit aggregate and proof")
	}
	r.metrics.ContinueDutyFullFlow()

	r.metrics.StartConsensus()
	byts, err := res.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal aggregate and proof")
	}
	input := &genesisspectypes.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}
	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(logger *zap.Logger, signedMsg *genesisspecqbft.SignedMessage) error {
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

	aggregateAndProof, err := decidedValue.GetAggregateAndProof()
	if err != nil {
		return errors.Wrap(err, "could not get aggregate and proof")
	}

	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(r, aggregateAndProof, decidedValue.Duty.Slot, genesisspectypes.DomainAggregateAndProof)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*genesisspectypes.PartialSignatureMessage{msg},
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast := &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.SSVPartialSignatureMsgType,
		MsgID:   genesisspectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}

	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *AggregatorRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	r.metrics.EndPostConsensus()

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		aggregateAndProof, err := r.GetState().DecidedValue.GetAggregateAndProof()
		if err != nil {
			return errors.Wrap(err, "could not get aggregate and proof")
		}

		msg := &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof,
			Signature: specSig,
		}

		proofSubmissionEnd := r.metrics.StartBeaconSubmission()

		if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(msg); err != nil {
			r.metrics.RoleSubmissionFailed()
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed aggregate")
		}

		proofSubmissionEnd()
		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		logger.Debug("✅ successful submitted aggregate")
	}
	r.GetState().Finished = true

	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{genesisspectypes.SSZUint64(r.GetState().StartingDuty.Slot)}, genesisspectypes.DomainSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AggregatorRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	aggregateAndProof, err := r.GetState().DecidedValue.GetAggregateAndProof()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get aggregate and proof")
	}

	return []ssz.HashRoot{aggregateAndProof}, genesisspectypes.DomainAggregateAndProof, nil
}

// executeDuty steps:
// 1) sign a partial selection proof and wait for 2f+1 partial sigs from peers
// 2) reconstruct selection proof and send SubmitAggregateSelectionProof to BN
// 3) start consensus on duty + aggregation data
// 4) Once consensus decides, sign partial aggregation data and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedAggregateSubmitRequest sig to the BN
func (r *AggregatorRunner) executeDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	r.metrics.StartDutyFullFlow()
	r.metrics.StartPreConsensus()

	// sign selection proof
	msg, err := r.BaseRunner.signBeaconObject(r, genesisspectypes.SSZUint64(duty.Slot), duty.Slot, genesisspectypes.DomainSelectionProof)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.SelectionProofPartialSig,
		Slot:     duty.Slot,
		Messages: []*genesisspectypes.PartialSignatureMessage{msg},
	}

	// sign msg
	signature, err := r.GetSigner().SignRoot(msgs, genesisspectypes.PartialSignatureType, r.GetShare().SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for selection proof")
	}
	signedPartialMsg := &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: signature,
		Signer:    r.GetShare().OperatorID,
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode selection proof pre-consensus signature msg")
	}
	msgToBroadcast := &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.SSVPartialSignatureMsgType,
		MsgID:   genesisspectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
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

func (r *AggregatorRunner) GetNetwork() genesisspecssv.Network {
	return r.network
}

func (r *AggregatorRunner) GetBeaconNode() genesisspecssv.BeaconNode {
	return r.beacon
}

func (r *AggregatorRunner) GetShare() *genesisspectypes.Share {
	return r.BaseRunner.Share
}

func (r *AggregatorRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *AggregatorRunner) GetValCheckF() genesisspecqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AggregatorRunner) GetSigner() genesisspectypes.KeyManager {
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
func (r *AggregatorRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// isAggregator returns true if the signature is from the input validator. The committee
// count is provided as an argument rather than imported implementation from spec. Having
// committee count as an argument allows cheaper computation at run time.
//
// Spec pseudocode definition:
//
//	def is_aggregator(state: BeaconState, slot: Slot, index: CommitteeIndex, slot_signature: BLSSignature) -> bool:
//	 committee = get_beacon_committee(state, slot, index)
//	 modulo = max(1, len(committee) // TARGET_AGGREGATORS_PER_COMMITTEE)
//	 return bytes_to_uint64(hash(slot_signature)[0:8]) % modulo == 0
func isAggregator(committeeCount uint64, slotSig []byte) (bool, error) {
	const targetAggregatorsPerCommittee uint64 = 16

	modulo := committeeCount / targetAggregatorsPerCommittee
	if modulo == 0 {
		// Modulo must be at least 1.
		modulo = 1
	}

	b := hashSha256(slotSig)
	return binary.LittleEndian.Uint64(b[:8])%modulo == 0, nil
}

var sha256Pool = sync.Pool{New: func() interface{} {
	return sha256.New()
}}

// hashSha256 defines a function that returns the sha256 checksum of the data passed in.
// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/core/0_beacon-chain.md#hash
func hashSha256(data []byte) [32]byte {
	h, ok := sha256Pool.Get().(hash.Hash)
	if !ok {
		h = sha256.New()
	}
	defer sha256Pool.Put(h)
	h.Reset()

	var b [32]byte

	// The hash interface never returns an error, for that reason
	// we are not handling the error below. For reference, it is
	// stated here https://golang.org/pkg/hash/#Hash

	// #nosec G104
	h.Write(data)
	h.Sum(b[:0])

	return b
}
