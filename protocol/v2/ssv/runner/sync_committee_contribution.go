package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type SyncCommitteeAggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF
	measurements   measurementsStore
}

func NewSyncCommitteeAggregatorRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &SyncCommitteeAggregatorRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleSyncCommitteeContribution,
			NetworkConfig:      networkConfig,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		valCheck:       valCheck,
		operatorSigner: operatorSigner,
		measurements:   NewMeasurementsStore(),
	}, nil
}

func (r *SyncCommitteeAggregatorRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *SyncCommitteeAggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *SyncCommitteeAggregatorRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_pre_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing sync committee selection proof message: %w", err)
	}

	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().CurrentDuty.DutySlot())
	recordSuccessfulQuorum(ctx, 1, epoch, spectypes.BNRoleSyncCommitteeContribution, attributeConsensusPhasePreConsensus)

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleSyncCommitteeContribution)

	// collect selection proofs and subnets
	//nolint: prealloc
	var (
		selectionProofs []phase0.BLSSignature
		subnets         []uint64
	)
	for i, root := range roots {
		// reconstruct selection proof sig
		span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return traces.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
		}

		blsSigSelectionProof := phase0.BLSSignature{}
		copy(blsSigSelectionProof[:], sig)

		aggregator := r.GetBeaconNode().IsSyncCommitteeAggregator(sig)
		if !aggregator {
			continue
		}

		// fetch sync committee contribution
		span.AddEvent("fetching sync committee subnet ID")
		subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(r.GetState().CurrentDuty.(*spectypes.ValidatorDuty).ValidatorSyncCommitteeIndices[i]))

		selectionProofs = append(selectionProofs, blsSigSelectionProof)
		subnets = append(subnets, subnet)
	}

	if len(selectionProofs) == 0 {
		span.AddEvent("no selection proofs")
		span.SetStatus(codes.Ok, "")
		r.GetState().Finished = true
		r.measurements.EndDutyFlow()
		return nil
	}

	duty := r.GetState().CurrentDuty.(*spectypes.ValidatorDuty)

	r.measurements.PauseDutyFlow()

	span.AddEvent("fetching sync committee contributions")
	contributions, ver, err := r.GetBeaconNode().GetSyncCommitteeContribution(ctx, duty.DutySlot(), selectionProofs, subnets)
	if err != nil {
		return traces.Errorf(span, "could not get sync committee contribution: %w", err)
	}
	r.measurements.ContinueDutyFlow()

	byts, err := contributions.MarshalSSZ()
	if err != nil {
		return traces.Errorf(span, "could not marshal sync committee contributions: %w", err)
	}

	// create consensus object
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	r.measurements.StartConsensus()
	if err := r.BaseRunner.decide(ctx, logger, r, input.Duty.Slot, input); err != nil {
		return traces.Errorf(span, "can't start new duty runner instance for duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *SyncCommitteeAggregatorRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_consensus"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(signedMsg.SSVMessage.GetID()),
			observability.ValidatorMsgTypeAttribute(signedMsg.SSVMessage.GetType()),
			observability.RunnerRoleAttribute(signedMsg.SSVMessage.GetID().GetRoleType()),
		))
	defer span.End()

	span.AddEvent("checking if instance is decided")
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r.GetValCheckF(), signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return traces.Errorf(span, "failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("instance is decided")
	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleSyncCommitteeContribution)

	r.measurements.StartPostConsensus()

	cd := decidedValue.(*spectypes.ValidatorConsensusData)
	span.SetAttributes(
		observability.BeaconSlotAttribute(cd.Duty.Slot),
		observability.ValidatorPublicKeyAttribute(cd.Duty.PubKey),
	)

	contributions, err := cd.GetSyncCommitteeContributions()
	if err != nil {
		return traces.Errorf(span, "could not get contributions: %w", err)
	}

	// specific duty sig
	msgs := make([]*spectypes.PartialSignatureMessage, 0)
	for _, c := range contributions {
		contribAndProof, _, err := r.generateContributionAndProof(ctx, c.Contribution, c.SelectionProofSig)
		if err != nil {
			return traces.Errorf(span, "could not generate contribution and proof: %w", err)
		}

		signed, err := signBeaconObject(
			ctx,
			r,
			r.BaseRunner.State.CurrentDuty.(*spectypes.ValidatorDuty),
			contribAndProof,
			cd.Duty.Slot,
			spectypes.DomainContributionAndProof,
		)
		if err != nil {
			return traces.Errorf(span, "failed to sign aggregate and proof: %w", err)
		}

		msgs = append(msgs, signed)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: msgs,
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)

	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode post consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSV partial signature message: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial post consensus sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *SyncCommitteeAggregatorRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, msg)
}

func (r *SyncCommitteeAggregatorRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_post_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing post consensus message: %w", err)
	}

	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleSyncCommitteeContribution)

	// get contributions
	validatorConsensusData := &spectypes.ValidatorConsensusData{}
	err = validatorConsensusData.Decode(r.GetState().DecidedValue)
	if err != nil {
		return traces.Errorf(span, "could not decode validator consensus data: %w", err)
	}
	contributions, err := validatorConsensusData.GetSyncCommitteeContributions()
	if err != nil {
		return traces.Errorf(span, "could not get contributions: %w", err)
	}

	var successfullySubmittedContributions uint32
	for _, root := range roots {
		span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return traces.Errorf(span, "got post-consensus quorum but it has invalid signatures: %w", err)
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		for _, contribution := range contributions {
			start := time.Now()
			// match the right contrib and proof root to signed root
			contribAndProof, contribAndProofRoot, err := r.generateContributionAndProof(ctx, contribution.Contribution, contribution.SelectionProofSig)
			if err != nil {
				return traces.Errorf(span, "could not generate contribution and proof: %w", err)
			}
			if !bytes.Equal(root[:], contribAndProofRoot[:]) {
				span.AddEvent("incorrect root, skipping")
				continue // not the correct root
			}

			signedContrib, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
			if err != nil {
				return traces.Errorf(span, "could not reconstruct contribution and proof sig: %w", err)
			}
			blsSignedContribAndProof := phase0.BLSSignature{}
			copy(blsSignedContribAndProof[:], signedContrib)
			signedContribAndProof := &altair.SignedContributionAndProof{
				Message:   contribAndProof,
				Signature: blsSignedContribAndProof,
			}

			if err := r.GetBeaconNode().SubmitSignedContributionAndProof(ctx, signedContribAndProof); err != nil {
				recordFailedSubmission(ctx, spectypes.BNRoleSyncCommitteeContribution)
				logger.Error("❌ could not submit to Beacon chain reconstructed contribution and proof",
					fields.SubmissionTime(time.Since(start)),
					zap.Error(err))
				return traces.Errorf(span, "could not submit to Beacon chain reconstructed contribution and proof: %w", err)
			}

			successfullySubmittedContributions++

			const eventMsg = "✅ successfully submitted sync committee aggregator"
			span.AddEvent(eventMsg)
			logger.Debug(
				eventMsg,
				fields.SubmissionTime(time.Since(start)),
				fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
				fields.TotalDutyTime(r.measurements.TotalDutyTime()),
			)
			break
		}
	}

	r.GetState().Finished = true
	r.measurements.EndDutyFlow()

	recordDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.BNRoleSyncCommitteeContribution, r.GetState().RunningInstance.State.Round)
	recordSuccessfulSubmission(
		ctx,
		successfullySubmittedContributions,
		r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().CurrentDuty.DutySlot()),
		spectypes.BNRoleSyncCommitteeContribution,
	)

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *SyncCommitteeAggregatorRunner) generateContributionAndProof(
	ctx context.Context,
	contrib altair.SyncCommitteeContribution,
	proof phase0.BLSSignature,
) (*altair.ContributionAndProof, phase0.Root, error) {
	contribAndProof := &altair.ContributionAndProof{
		AggregatorIndex: r.GetState().CurrentDuty.(*spectypes.ValidatorDuty).ValidatorIndex,
		Contribution:    &contrib,
		SelectionProof:  proof,
	}

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().CurrentDuty.DutySlot())
	dContribAndProof, err := r.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainContributionAndProof)
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
	for _, index := range r.GetState().CurrentDuty.(*spectypes.ValidatorDuty).ValidatorSyncCommitteeIndices {
		subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              r.GetState().CurrentDuty.DutySlot(),
			SubcommitteeIndex: subnet,
		}
		sszIndexes = append(sszIndexes, data)
	}
	return sszIndexes, spectypes.DomainSyncCommitteeSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *SyncCommitteeAggregatorRunner) expectedPostConsensusRootsAndDomain(ctx context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	// get contributions
	validatorConsensusData := &spectypes.ValidatorConsensusData{}
	err := validatorConsensusData.Decode(r.GetState().DecidedValue)
	if err != nil {
		return nil, spectypes.DomainError, errors.Wrap(err, "could not create consensus data")
	}
	contributions, err := validatorConsensusData.GetSyncCommitteeContributions()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get contributions")
	}

	ret := make([]ssz.HashRoot, 0)
	for _, contrib := range contributions {
		contribAndProof, _, err := r.generateContributionAndProof(ctx, contrib.Contribution, contrib.SelectionProofSig)
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
func (r *SyncCommitteeAggregatorRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	r.measurements.StartDutyFlow()
	r.measurements.StartPreConsensus()

	// sign selection proofs
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	for _, index := range r.GetState().CurrentDuty.(*spectypes.ValidatorDuty).ValidatorSyncCommitteeIndices {
		subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              duty.DutySlot(),
			SubcommitteeIndex: subnet,
		}
		span.AddEvent("signing beacon object")
		msg, err := signBeaconObject(
			ctx,
			r,
			duty.(*spectypes.ValidatorDuty),
			data,
			duty.DutySlot(),
			spectypes.DomainSyncCommitteeSelectionProof,
		)
		if err != nil {
			return traces.Errorf(span, "could not sign sync committee selection proof: %w", err)
		}

		msgs.Messages = append(msgs.Messages, msg)
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode partial signature messages: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting signed SSV message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial contribution proof sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *SyncCommitteeAggregatorRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *SyncCommitteeAggregatorRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *SyncCommitteeAggregatorRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *SyncCommitteeAggregatorRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *SyncCommitteeAggregatorRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *SyncCommitteeAggregatorRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *SyncCommitteeAggregatorRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *SyncCommitteeAggregatorRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
}

func (r *SyncCommitteeAggregatorRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *SyncCommitteeAggregatorRunner) GetNetworkConfig() *networkconfig.Network {
	return r.BaseRunner.NetworkConfig
}

func (r *SyncCommitteeAggregatorRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *SyncCommitteeAggregatorRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *SyncCommitteeAggregatorRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *SyncCommitteeAggregatorRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *SyncCommitteeAggregatorRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}
func (r *SyncCommitteeAggregatorRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
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
func (r *SyncCommitteeAggregatorRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode SyncCommitteeAggregatorRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
