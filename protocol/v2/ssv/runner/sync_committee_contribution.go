package runner

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type SyncCommitteeAggregatorRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	measurements   dutyMeasurements

	// ValCheck is used to validate the qbft-value(s) proposed by other Operators.
	ValCheck ssv.ValueChecker

	// rootToSyncCommitteeIdx is the root->validator_sync_committee_index mapping for the current duty.
	rootToSyncCommitteeIdx map[phase0.Root]phase0.ValidatorIndex
}

// SyncCommitteeAggregatorRunnerOptions bundles all dependencies required by NewSyncCommitteeAggregatorRunner.
type SyncCommitteeAggregatorRunnerOptions struct {
	BaseRunnerOptions

	QBFTController     *controller.Controller
	ValCheck           ssv.ValueChecker
	HighestDecidedSlot phase0.Slot
}

func NewSyncCommitteeAggregatorRunner(opts SyncCommitteeAggregatorRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &SyncCommitteeAggregatorRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     ssvtypes.RoleSyncCommitteeContribution,
			NetworkConfig:      opts.NetworkConfig,
			Share:              opts.Share,
			QBFTController:     opts.QBFTController,
			highestDecidedSlot: opts.HighestDecidedSlot,
		},

		beacon:         opts.Beacon,
		network:        opts.Network,
		signer:         opts.Signer,
		ValCheck:       opts.ValCheck,
		operatorSigner: opts.OperatorSigner,
		measurements:   *newMeasurementsStore(),
	}, nil
}

func (r *SyncCommitteeAggregatorRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	return r.baseStartNewDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *SyncCommitteeAggregatorRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing sync committee selection proof message: %w", err)
	}

	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), ssvtypes.RoleSyncCommitteeContribution)

	// Collect (subnet, selection-proof) pairs. Pairing them in a single slice keeps
	// subnet and proof together by construction — there's no second slice to fall out
	// of sync, so no length invariant to guard.
	pairs := make([]subnetSelectionProof, 0, len(roots))
	for _, root := range roots {
		// reconstruct selection proof sig
		span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		sig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
		}

		blsSigSelectionProof := phase0.BLSSignature{}
		copy(blsSigSelectionProof[:], sig)

		aggregator := r.GetBeaconNode().IsSyncCommitteeAggregator(sig)
		if !aggregator {
			continue
		}

		// fetch sync committee contribution
		vIdx, ok := r.rootToSyncCommitteeIdx[root]
		if !ok {
			logger.Warn("root got a quorum, but is unknown to us", fields.Root(root))
			continue
		}
		subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(vIdx))

		pairs = append(pairs, subnetSelectionProof{subnet: subnet, selectionProof: blsSigSelectionProof})
	}

	// Sort by ascending subnet so the resulting Contributions slice has a
	// deterministic, spec-canonical order. See sortBySubnet for the full rationale.
	sortBySubnet(pairs)

	if len(pairs) == 0 {
		r.markDutyNotRequired()
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), ssvtypes.RoleSyncCommitteeContribution, 0)
		const dutyFinishedNoProofsEvent = "✔️successfully finished duty processing (no selection proofs)"
		logger.Info(dutyFinishedNoProofsEvent,
			fields.PreConsensusTime(r.measurements.PreConsensusTime()),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(r.measurements.TotalDutyTime()),
		)
		span.AddEvent(dutyFinishedNoProofsEvent)
		return nil
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// GetSyncCommitteeContribution takes the proofs and subnets as parallel slices;
	// split the sorted pairs back out at the call boundary.
	selectionProofs := make([]phase0.BLSSignature, len(pairs))
	subnets := make([]uint64, len(pairs))
	for i, p := range pairs {
		selectionProofs[i], subnets[i] = p.selectionProof, p.subnet
	}

	span.AddEvent("fetching sync committee contributions")
	contributions, ver, err := r.GetBeaconNode().GetSyncCommitteeContribution(ctx, duty.DutySlot(), selectionProofs, subnets)
	if err != nil {
		return fmt.Errorf("could not get sync committee contribution: %w", err)
	}

	byts, err := contributions.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("could not marshal sync committee contributions: %w", err)
	}

	// create consensus object
	input := &spectypes.ProposerConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	r.measurements.StartConsensus()
	if err := r.decide(ctx, logger, input.Duty.Slot, input, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}

	return nil
}

func (r *SyncCommitteeAggregatorRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("processing QBFT consensus msg")
	decided, decidedValue, err := r.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, signedMsg, &spectypes.ProposerConsensusData{})
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), ssvtypes.RoleSyncCommitteeContribution)

	cd := decidedValue.(*spectypes.ProposerConsensusData)
	span.SetAttributes(
		observability.BeaconSlotAttribute(cd.Duty.Slot),
		observability.ValidatorPublicKeyAttribute(cd.Duty.PubKey),
	)

	contributions, err := ssvtypes.GetSyncCommitteeContributions(cd)
	if err != nil {
		return fmt.Errorf("could not get contributions: %w", err)
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// specific duty sig
	msgs := make([]*spectypes.PartialSignatureMessage, 0)
	for _, c := range contributions {
		contribAndProof, _, err := r.generateContributionAndProof(ctx, c.Contribution, c.SelectionProofSig)
		if err != nil {
			return fmt.Errorf("could not generate contribution and proof: %w", err)
		}

		signed, err := signBeaconObject(
			ctx,
			r,
			r.NetworkConfig,
			duty,
			contribAndProof,
			cd.Duty.Slot,
			spectypes.DomainContributionAndProof,
		)
		if err != nil {
			return fmt.Errorf("failed to sign aggregate and proof: %w", err)
		}

		msgs = append(msgs, signed)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: msgs,
	}

	domain := r.NetworkConfig.DomainTypeAtSlot(cd.Duty.Slot)
	msgID := spectypes.NewMsgID(domain, r.GetShare().ValidatorPubKey[:], r.RunnerRoleType)

	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return fmt.Errorf("could not encode post consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSV partial signature message: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	r.measurements.StartPostConsensus()
	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().BroadcastAtSlot(msgToBroadcast, postConsensusMsg.Slot); err != nil {
		return fmt.Errorf("can't broadcast partial post consensus sig: %w", err)
	}
	const broadcastedPostConsensusMsgEvent = "broadcasted post-consensus partial signature message"
	logger.Debug(broadcastedPostConsensusMsgEvent)
	span.AddEvent(broadcastedPostConsensusMsgEvent)

	return nil
}

func (r *SyncCommitteeAggregatorRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}

	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), ssvtypes.RoleSyncCommitteeContribution)

	// get contributions
	validatorConsensusData := &spectypes.ProposerConsensusData{}
	err = validatorConsensusData.Decode(r.State.DecidedValue)
	if err != nil {
		return fmt.Errorf("could not decode decided validator consensus data: %w", err)
	}
	contributions, err := ssvtypes.GetSyncCommitteeContributions(validatorConsensusData)
	if err != nil {
		return fmt.Errorf("could not get contributions: %w", err)
	}

	const submittingSyncCommitteeEvent = "submitting sync committee contributions"
	span.AddEvent(submittingSyncCommitteeEvent)
	logger.Debug(submittingSyncCommitteeEvent)

	successfullySubmittedContributions := int64(0)
	start := time.Now()
	for _, root := range roots {
		span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		sig, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.FallBackAndVerifyEachSignature(r.State.PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return spectypes.WrapError(
				spectypes.PostConsensusQuorumWithInvalidSignatures,
				fmt.Errorf("got post-consensus quorum but it has invalid signatures: %w", err),
			)
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		for _, contribution := range contributions {
			// match the right contrib and proof root to signed root
			contribAndProof, contribAndProofRoot, err := r.generateContributionAndProof(ctx, contribution.Contribution, contribution.SelectionProofSig)
			if err != nil {
				return fmt.Errorf("could not generate contribution and proof: %w", err)
			}
			if !bytes.Equal(root[:], contribAndProofRoot[:]) {
				span.AddEvent("incorrect root, skipping")
				continue // not the correct root
			}

			signedContrib, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
			if err != nil {
				return fmt.Errorf("could not reconstruct contribution and proof sig: %w", err)
			}
			blsSignedContribAndProof := phase0.BLSSignature{}
			copy(blsSignedContribAndProof[:], signedContrib)
			signedContribAndProof := &altair.SignedContributionAndProof{
				Message:   contribAndProof,
				Signature: blsSignedContribAndProof,
			}

			const submittingSyncCommitteeEvent = "submitting sync committee contribution"
			span.AddEvent(submittingSyncCommitteeEvent)
			logger.Debug(submittingSyncCommitteeEvent)

			reqStart := time.Now()
			err = r.GetBeaconNode().SubmitSignedContributionAndProof(ctx, signedContribAndProof)
			if err != nil {
				recordFailedSubmission(ctx, spectypes.BNRoleSyncCommitteeContribution)
				logger.Error("❌ could not submit to Beacon chain reconstructed contribution and proof",
					fields.Took(time.Since(reqStart)),
					zap.Error(err),
				)
				return fmt.Errorf("could not submit to Beacon chain reconstructed contribution and proof: %w", err)
			}

			successfullySubmittedContributions++

			const submittedSyncCommitteeEvent = "successfully submitted sync committee contribution"
			span.AddEvent(submittedSyncCommitteeEvent)
			logger.Debug(submittedSyncCommitteeEvent, fields.Took(time.Since(reqStart)))

			break
		}
	}
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}
	recordSuccessfulSubmission(ctx, successfullySubmittedContributions, r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot), spectypes.BNRoleSyncCommitteeContribution)
	const submittedSyncCommitteeEvent = "✅ successfully submitted sync committee contributions"
	span.AddEvent(submittedSyncCommitteeEvent)
	logger.Debug(submittedSyncCommitteeEvent,
		zap.Int64("submitted_contributions", successfullySubmittedContributions),
		fields.Took(time.Since(start)),
	)

	r.markDutySucceeded()
	r.measurements.EndDutyFlow()
	recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), ssvtypes.RoleSyncCommitteeContribution, r.State.RunningInstance.State.Round)
	const dutyFinishedEvent = "✔️successfully finished duty processing"
	logger.Info(dutyFinishedEvent,
		fields.PreConsensusTime(r.measurements.PreConsensusTime()),
		fields.ConsensusTime(r.measurements.ConsensusTime()),
		fields.ConsensusRounds(uint64(r.State.RunningInstance.State.Round)),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)
	span.AddEvent(dutyFinishedEvent)

	return nil
}

func (r *SyncCommitteeAggregatorRunner) generateContributionAndProof(
	ctx context.Context,
	contrib altair.SyncCommitteeContribution,
	proof phase0.BLSSignature,
) (*altair.ContributionAndProof, phase0.Root, error) {
	duty, err := r.currentValidatorDuty()
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("current validator duty: %w", err)
	}

	contribAndProof := &altair.ContributionAndProof{
		AggregatorIndex: duty.ValidatorIndex,
		Contribution:    &contrib,
		SelectionProof:  proof,
	}

	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("current duty slot: %w", err)
	}
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot)
	dContribAndProof, err := r.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainContributionAndProof)
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("could not get domain data: %w", err)
	}
	contribAndProofRoot, err := spectypes.ComputeETHSigningRoot(contribAndProof, dContribAndProof)
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("could not compute signing root: %w", err)
	}
	return contribAndProof, contribAndProofRoot, nil
}

func (r *SyncCommitteeAggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	duty, err := r.currentValidatorDuty()
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("current validator duty: %w", err)
	}
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("current duty slot: %w", err)
	}
	indices := duty.ValidatorSyncCommitteeIndices
	sszIndexes := make([]ssz.HashRoot, 0, len(indices))
	for _, index := range indices {
		subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              currentDutySlot,
			SubcommitteeIndex: subnet,
		}
		sszIndexes = append(sszIndexes, data)
	}
	return sszIndexes, spectypes.DomainSyncCommitteeSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *SyncCommitteeAggregatorRunner) expectedPostConsensusRootsAndDomain(ctx context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	// get contributions
	validatorConsensusData := &spectypes.ProposerConsensusData{}
	err := validatorConsensusData.Decode(r.State.DecidedValue)
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("could not create consensus data: %w", err)
	}
	contributions, err := ssvtypes.GetSyncCommitteeContributions(validatorConsensusData)
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not get contributions: %w", err)
	}

	ret := make([]ssz.HashRoot, 0)
	for _, contrib := range contributions {
		contribAndProof, _, err := r.generateContributionAndProof(ctx, contrib.Contribution, contrib.SelectionProofSig)
		if err != nil {
			return nil, spectypes.DomainError, fmt.Errorf("could not generate contribution and proof: %w", err)
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
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	r.measurements.StartDutyFlow()

	// sign selection proofs
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     ssvtypes.ContributionProofs,
		Slot:     validatorDuty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	// re-build the root->validator mapping for this duty
	r.rootToSyncCommitteeIdx = make(map[phase0.Root]phase0.ValidatorIndex)

	for _, vIdx := range validatorDuty.ValidatorSyncCommitteeIndices {
		subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(vIdx))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              validatorDuty.DutySlot(),
			SubcommitteeIndex: subnet,
		}
		span.AddEvent("signing beacon object")
		msg, err := signBeaconObject(
			ctx,
			r,
			r.NetworkConfig,
			validatorDuty,
			data,
			validatorDuty.DutySlot(),
			spectypes.DomainSyncCommitteeSelectionProof,
		)
		if err != nil {
			return fmt.Errorf("could not sign sync committee selection proof: %w", err)
		}

		msgs.Messages = append(msgs.Messages, msg)

		r.rootToSyncCommitteeIdx[msg.SigningRoot] = phase0.ValidatorIndex(vIdx)
	}

	logger.Debug("signing and broadcasting contribution proof partial sig", fields.Slot(validatorDuty.DutySlot()))

	r.measurements.StartPreConsensus()
	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast contribution proof partial sig: %w", err)
	}

	return nil
}

func (r *SyncCommitteeAggregatorRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *SyncCommitteeAggregatorRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *SyncCommitteeAggregatorRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.Share {
		return share
	}
	return nil
}

func (r *SyncCommitteeAggregatorRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *SyncCommitteeAggregatorRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *SyncCommitteeAggregatorRunner) MarshalJSON() ([]byte, error) {
	type syncCommitteeAggregatorRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
		// ValCheck is intentionally kept in the JSON to preserve the historical runner state shape
		// (and thus runner state roots used by spec tests). It is a runtime-only dependency and
		// is ignored on decode, so it is always marshaled as `null` for determinism.
		ValCheck any `json:"ValCheck"`
	}

	return json.Marshal(&syncCommitteeAggregatorRunnerJSON{
		BaseRunner: r.BaseRunner,
		ValCheck:   nil,
	})
}

func (r *SyncCommitteeAggregatorRunner) UnmarshalJSON(data []byte) error {
	type syncCommitteeAggregatorRunnerJSON struct {
		BaseRunner *BaseRunner     `json:"BaseRunner"`
		ValCheck   json.RawMessage `json:"ValCheck"`
	}

	aux := &syncCommitteeAggregatorRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}

	r.BaseRunner = aux.BaseRunner
	// ValCheck is not restored from JSON. Callers must rehydrate it explicitly.
	r.ValCheck = nil
	return nil
}

// Encode returns the encoded struct in bytes or error
func (r *SyncCommitteeAggregatorRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *SyncCommitteeAggregatorRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

// GetRoot returns the root used for signing and verification
func (r *SyncCommitteeAggregatorRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode SyncCommitteeAggregatorRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// subnetSelectionProof pairs a reconstructed selection-proof signature with the
// sync-committee subnet it belongs to. Keeping them in one slice (rather than two
// parallel slices) makes the (subnet, proof) pairing unbreakable by construction.
type subnetSelectionProof struct {
	subnet         uint64
	selectionProof phase0.BLSSignature
}

// sortBySubnet canonicalizes the pre-consensus output before calling
// GetSyncCommitteeContribution by sorting in-place by ascending subnet, so the resulting
// Contributions slice has a deterministic, spec-aligned order.
//
// Without this normalization, the upstream `roots` slice from basePreConsensusMsgProcessing
// is built via `slices.Collect(maps.Keys(...))`, which Go randomizes per process. Two SSV
// nodes can then produce different Contributions SSZ roots for the same logical
// contribution set. The spec's de-facto canonical ordering is ascending SubcommitteeIndex
// (see ssv-spec/types/testingutils/beacon_node_sync_committee.go test fixtures).
//
// Subnet alone is a total order here, so the (unstable) slices.SortFunc needs no
// tiebreaker: each pre-consensus root is the hash of
// SyncAggregatorSelectionData{Slot, SubcommitteeIndex: subnet} (see executeDuty), so two
// sync-committee indices that map to the same subnet collapse to a single root — pairs
// never holds duplicate subnets. If that selection data ever becomes keyed by validator
// index instead of subnet, add a deterministic tiebreaker (e.g. selection-proof bytes).
func sortBySubnet(pairs []subnetSelectionProof) {
	slices.SortFunc(pairs, func(a, b subnetSelectionProof) int {
		return cmp.Compare(a.subnet, b.subnet)
	})
}
