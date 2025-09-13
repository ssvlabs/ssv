package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	types "github.com/wealdtech/go-eth2-types/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type AggregatorCommitteeRunner struct {
	BaseRunner     *BaseRunner
	network        specqbft.Network
	beacon         beacon.BeaconNode
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	//TODO(Aleg) not sure we need it
	//DutyGuard           CommitteeDutyGuard
	measurements measurementsStore

	// For aggregator role: tracks by validator index only (one submission per validator)
	// For sync committee contribution role: tracks by validator index and root (multiple submissions per validator)
	submittedDuties map[spectypes.BeaconRole]map[phase0.ValidatorIndex]map[[32]byte]struct{}
}

func NewAggregatorCommitteeRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
) (Runner, error) {
	if len(share) == 0 {
		return nil, errors.New("no shares")
	}

	return &AggregatorCommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleAggregatorCommittee,
			NetworkConfig:  networkConfig,
			Share:          share,
			QBFTController: qbftController,
		},
		beacon:          beacon,
		network:         network,
		signer:          signer,
		operatorSigner:  operatorSigner,
		valCheck:        valCheck,
		submittedDuties: make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]map[[32]byte]struct{}),
		measurements:    NewMeasurementsStore(),
	}, nil
}

func (r *AggregatorCommitteeRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.start_aggregator_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	d, ok := duty.(*spectypes.AggregatorCommitteeDuty)
	if !ok {
		return traces.Errorf(span, "duty is not a CommitteeDuty: %T", duty)
	}

	span.SetAttributes(observability.DutyCountAttribute(len(d.ValidatorDuties)))
	err := r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
	if err != nil {
		return traces.Error(span, err)
	}

	r.submittedDuties[spectypes.BNRoleAggregator] = make(map[phase0.ValidatorIndex]map[[32]byte]struct{})
	r.submittedDuties[spectypes.BNRoleSyncCommitteeContribution] = make(map[phase0.ValidatorIndex]map[[32]byte]struct{})

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorCommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *AggregatorCommitteeRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

func (r *AggregatorCommitteeRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode CommitteeRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (r *AggregatorCommitteeRunner) MarshalJSON() ([]byte, error) {
	type CommitteeRunnerAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       specqbft.ProposedValueCheckF
	}

	// Create object and marshal
	alias := &CommitteeRunnerAlias{
		BaseRunner:     r.BaseRunner,
		beacon:         r.beacon,
		network:        r.network,
		signer:         r.signer,
		operatorSigner: r.operatorSigner,
		valCheck:       r.valCheck,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

func (r *AggregatorCommitteeRunner) UnmarshalJSON(data []byte) error {
	type CommitteeRunnerAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       specqbft.ProposedValueCheckF
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeRunnerAlias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Assign fields
	r.BaseRunner = aux.BaseRunner
	r.beacon = aux.beacon
	r.network = aux.network
	r.signer = aux.signer
	r.operatorSigner = aux.operatorSigner
	r.valCheck = aux.valCheck
	return nil
}

func (r *AggregatorCommitteeRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AggregatorCommitteeRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *AggregatorCommitteeRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AggregatorCommitteeRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *AggregatorCommitteeRunner) GetBeaconSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *AggregatorCommitteeRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

// findValidatorDuty finds the validator duty for a specific role
func (r *AggregatorCommitteeRunner) findValidatorDuty(duty *spectypes.AggregatorCommitteeDuty, validatorIndex phase0.ValidatorIndex, role spectypes.BeaconRole) *spectypes.ValidatorDuty {
	for _, d := range duty.ValidatorDuties {
		if d.ValidatorIndex == validatorIndex && d.Type == role {
			return d
		}
	}

	return nil
}

// processAggregatorSelectionProof handles aggregator selection proofs
func (r *AggregatorCommitteeRunner) processAggregatorSelectionProof(
	ctx context.Context,
	selectionProof phase0.BLSSignature,
	vDuty *spectypes.ValidatorDuty,
	aggregatorData *spectypes.AggregatorCommitteeConsensusData,
) (bool, error) {
	isAggregator := r.beacon.IsAggregator(ctx, vDuty.Slot, vDuty.CommitteeIndex, vDuty.CommitteeLength, selectionProof[:])
	if !isAggregator {
		return false, nil
	}

	// TODO: waitToSlotTwoThirds(vDuty.Slot)

	attestation, err := r.beacon.GetAggregateAttestation(vDuty.Slot, vDuty.CommitteeIndex)
	if err != nil {
		return true, errors.Wrap(err, "failed to get aggregate attestation")
	}

	aggregatorData.Aggregators = append(aggregatorData.Aggregators, types.AssignedAggregator{
		ValidatorIndex: vDuty.ValidatorIndex,
		SelectionProof: selectionProof,
		CommitteeIndex: uint64(vDuty.CommitteeIndex),
	})

	// Marshal attestation for storage
	attestationBytes, err := attestation.MarshalSSZ()
	if err != nil {
		return true, errors.Wrap(err, "failed to marshal attestation")
	}

	aggregatorData.AggregatorsCommitteeIndexes = append(aggregatorData.AggregatorsCommitteeIndexes, uint64(vDuty.CommitteeIndex))
	aggregatorData.Attestations = append(aggregatorData.Attestations, attestationBytes)

	return true, nil
}

func (r *AggregatorCommitteeRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_pre_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing selection proof message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleAggregatorCommittee)

	aggregatorMap, contributionMap, err := r.expectedPreConsensusRoots(ctx)
	if err != nil {
		return traces.Errorf(span, "could not get expected pre-consensus roots: %w", err)
	}

	duty := r.BaseRunner.State.StartingDuty.(*spectypes.AggregatorCommitteeDuty)
	//epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	aggregatorData := &spectypes.AggregatorCommitteeConsensusData{
		//Version: r.beacon.DataVersion(epoch),
	}
	hasAnyAggregator := false

	rootSet := make(map[[32]byte]struct{})
	for _, root := range roots {
		rootSet[root] = struct{}{}
	}

	var sortedRoots [][32]byte
	for root := range rootSet {
		sortedRoots = append(sortedRoots, root)
	}
	// TODO(Aleg) why do we need it?
	sort.Slice(sortedRoots, func(i, j int) bool {
		return bytes.Compare(sortedRoots[i][:], sortedRoots[j][:]) < 0
	})

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(rootSet)))

	var anyErr error
	for _, root := range sortedRoots {
		metadataList, found := findValidatorsForPreConsensusRoot(root, aggregatorMap, contributionMap)
		if !found {
			// Edge case: since operators may have divergent sets of validators,
			// it's possible that an operator doesn't have the validator associated to a root.
			// In this case, we simply continue.
			continue
		}

		// TODO(Aleg) why this sort? why not root sort?
		sort.Slice(metadataList, func(i, j int) bool {
			return metadataList[i].ValidatorIndex < metadataList[j].ValidatorIndex
		})

		for _, metadata := range metadataList {
			validatorIndex := metadata.ValidatorIndex
			//TODO(Aleg) decide if we need to keep this validation here
			share := r.BaseRunner.Share[validatorIndex]
			if share == nil {
				continue
			}

			if !r.BaseRunner.State.PreConsensusContainer.HasQuorum(validatorIndex, root) {
				continue
			}

			// Reconstruct signature
			fullSig, err := r.BaseRunner.State.ReconstructBeaconSig(
				r.BaseRunner.State.PreConsensusContainer,
				root,
				share.ValidatorPubKey[:],
				validatorIndex,
			)
			if err != nil {
				// Fallback: verify each signature individually for all roots
				for root := range rootSet {
					r.BaseRunner.FallBackAndVerifyEachSignature(
						r.BaseRunner.State.PreConsensusContainer,
						root,
						share.Committee,
						validatorIndex,
					)
				}
				// TODO(Aleg) align to new committee runner
				// Record the error and continue to next validators
				const eventMsg = "got pre-consensus quorum but it has invalid signatures"
				span.AddEvent(eventMsg)
				logger.Error(eventMsg, fields.Slot(duty.Slot), zap.Error(err))
				anyErr = err
				continue
			}

			var blsSig phase0.BLSSignature
			copy(blsSig[:], fullSig)

			switch metadata.Role {
			case spectypes.BNRoleAggregator:
				vDuty := r.findValidatorDuty(duty, validatorIndex, spectypes.BNRoleAggregator)
				if vDuty != nil {
					isAggregator, err := r.processAggregatorSelectionProof(blsSig, vDuty, aggregatorData)
					if err == nil {
						if isAggregator {
							hasAnyAggregator = true
						}
					} else {
						anyErr = errors.Wrap(err, "failed to process aggregator selection proof")
					}
				}

			case spectypes.BNRoleSyncCommitteeContribution:
				vDuty := r.findValidatorDuty(duty, validatorIndex, spectypes.BNRoleSyncCommitteeContribution)
				if vDuty != nil {
					isAggregator, err := r.processSyncCommitteeSelectionProof(blsSig, metadata.SyncCommitteeIndex, vDuty, aggregatorData)
					if err == nil {
						if isAggregator {
							hasAnyAggregator = true
						}
					} else {
						anyErr = errors.Wrap(err, "failed to process sync committee selection proof")
					}
				}

			default:
				// This should never happen as we build rootToMetadata ourselves with valid roles
				return errors.Errorf("unexpected role type in pre-consensus metadata: %v", metadata.Role)
			}
		}
	}

	// only 1 root, verified by expectedPreConsensusRootsAndDomain
	root := roots[0]

	// reconstruct selection proof sig
	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return traces.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
	}

	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)
	span.SetAttributes(
		observability.CommitteeIndexAttribute(duty.CommitteeIndex),
		observability.ValidatorIndexAttribute(duty.ValidatorIndex),
	)

	const eventMsg = "🧩 got partial signature quorum"
	span.AddEvent(eventMsg, trace.WithAttributes(observability.ValidatorSignerAttribute(signedMsg.Messages[0].Signer)))
	logger.Debug(eventMsg,
		zap.Any("signer", signedMsg.Messages[0].Signer), // TODO: always 1?
		fields.Slot(duty.Slot),
	)

	r.measurements.PauseDutyFlow()

	span.AddEvent("submitting aggregate and proof",
		trace.WithAttributes(
			observability.CommitteeIndexAttribute(duty.CommitteeIndex),
			observability.ValidatorIndexAttribute(duty.ValidatorIndex)))
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(ctx, duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return traces.Errorf(span, "failed to submit aggregate and proof: %w", err)
	}
	r.measurements.ContinueDutyFlow()

	byts, err := res.MarshalSSZ()
	if err != nil {
		return traces.Errorf(span, "could not marshal aggregate and proof: %w", err)
	}
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(ctx, logger, r, duty.Slot, input); err != nil {
		return traces.Errorf(span, "can't start new duty runner instance for duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorCommitteeRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_committee_consensus"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(msg.SSVMessage.GetID()),
			observability.ValidatorMsgTypeAttribute(msg.SSVMessage.GetType()),
			observability.RunnerRoleAttribute(msg.SSVMessage.GetID().GetRoleType()),
		))
	defer span.End()

	span.AddEvent("checking if instance is decided")
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r, msg, &spectypes.BeaconVote{})
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
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleCommittee)

	r.measurements.StartPostConsensus()

	duty := r.BaseRunner.State.StartingDuty
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	version, _ := r.BaseRunner.NetworkConfig.ForkAtEpoch(epoch)

	committeeDuty, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		return traces.Errorf(span, "duty is not a CommitteeDuty: %T", duty)
	}

	span.SetAttributes(
		observability.BeaconSlotAttribute(duty.DutySlot()),
		observability.BeaconEpochAttribute(epoch),
		observability.BeaconVersionAttribute(version),
		observability.DutyCountAttribute(len(committeeDuty.ValidatorDuties)),
	)

	span.AddEvent("signing validator duties")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg sync.WaitGroup
		// errCh is buffered because the receiver is only interested in the very 1st error sent to this channel
		// and will not read any subsequent errors. Buffering ensures that senders can send their errors and terminate without being blocked,
		// regardless of whether the receiver is still actively reading from the channel.
		errCh        = make(chan error, len(committeeDuty.ValidatorDuties))
		signaturesCh = make(chan *spectypes.PartialSignatureMessage)
		dutiesCh     = make(chan *spectypes.ValidatorDuty)

		beaconVote = decidedValue.(*spectypes.BeaconVote)
		totalAttesterDuties,
		totalSyncCommitteeDuties,
		blockedAttesterDuties atomic.Uint32
	)

	// The worker pool will throttle the parallel processing of validator duties.
	// This is mainly needed because the processing involves several outgoing HTTP calls to the Consensus Client.
	// These calls should be limited to a certain degree to reduce the pressure on the Consensus Node.
	const workerCount = 30

	go func() {
		defer close(dutiesCh)
		for _, duty := range committeeDuty.ValidatorDuties {
			if ctx.Err() != nil {
				break
			}
			dutiesCh <- duty
		}
	}()

	for range workerCount {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for validatorDuty := range dutiesCh {
				if ctx.Err() != nil {
					return
				}

				switch validatorDuty.Type {
				case spectypes.BNRoleAttester:
					totalAttesterDuties.Add(1)
					isAttesterDutyBlocked, partialSigMsg, err := r.signAttesterDuty(ctx, validatorDuty, beaconVote, version, logger)
					if err != nil {
						errCh <- fmt.Errorf("failed signing attestation data: %w", err)
						return
					}
					if isAttesterDutyBlocked {
						blockedAttesterDuties.Add(1)
						continue
					}

					signaturesCh <- partialSigMsg
				case spectypes.BNRoleSyncCommittee:
					totalSyncCommitteeDuties.Add(1)

					partialSigMsg, err := r.BaseRunner.signBeaconObject(
						ctx,
						r,
						validatorDuty,
						spectypes.SSZBytes(beaconVote.BlockRoot[:]),
						validatorDuty.DutySlot(),
						spectypes.DomainSyncCommittee,
					)
					if err != nil {
						errCh <- fmt.Errorf("failed signing sync committee message: %w", err)
						return
					}

					signaturesCh <- partialSigMsg
				default:
					errCh <- fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(signaturesCh)
	}()

listener:
	for {
		select {
		case err := <-errCh:
			cancel()
			return traces.Error(span, err)
		case signature, ok := <-signaturesCh:
			if !ok {
				break listener
			}
			postConsensusMsg.Messages = append(postConsensusMsg.Messages, signature)
		}
	}

	var (
		totalAttestations   = totalAttesterDuties.Load()
		totalSyncCommittee  = totalSyncCommitteeDuties.Load()
		blockedAttestations = blockedAttesterDuties.Load()
	)

	if totalAttestations == 0 && totalSyncCommittee == 0 {
		r.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDuties.Error())
		return ErrNoValidDuties
	}

	// Avoid sending an empty message if all attester duties were blocked due to Doppelganger protection
	// and no sync committee duties exist.
	//
	// We do not mark the state as finished here because post-consensus messages must still be processed,
	// allowing validators to be marked as safe once sufficient consensus is reached.
	if totalAttestations == blockedAttestations && totalSyncCommittee == 0 {
		const eventMsg = "Skipping message broadcast: all attester duties blocked by Doppelganger protection, no sync committee duties."
		span.AddEvent(eventMsg)
		logger.Debug(eventMsg,
			zap.Uint32("attester_duties", totalAttestations),
			zap.Uint32("blocked_attesters", blockedAttestations))

		span.SetStatus(codes.Ok, "")
		return nil
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID: spectypes.NewMsgID(
			r.BaseRunner.NetworkConfig.DomainType,
			r.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:],
			r.BaseRunner.RunnerRoleType,
		),
	}
	ssvMsg.Data, err = postConsensusMsg.Encode()
	if err != nil {
		return traces.Errorf(span, "failed to encode post consensus signature msg: %w", err)
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.BaseRunner.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial post consensus sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorCommitteeRunner) signAttesterDuty(
	ctx context.Context,
	validatorDuty *spectypes.ValidatorDuty,
	beaconVote *spectypes.BeaconVote,
	version spec.DataVersion,
	logger *zap.Logger) (isBlocked bool, partialSig *spectypes.PartialSignatureMessage, err error) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.sign_attester_duty"),
		trace.WithAttributes(
			observability.ValidatorIndexAttribute(validatorDuty.ValidatorIndex),
			observability.ValidatorPublicKeyAttribute(validatorDuty.PubKey),
			observability.BeaconRoleAttribute(validatorDuty.Type),
		))
	defer span.End()

	span.AddEvent("doppelganger: checking if signing is allowed")

	attestationData := constructAttestationData(beaconVote, validatorDuty, version)

	span.AddEvent("signing beacon object")
	partialMsg, err := r.BaseRunner.signBeaconObject(
		ctx,
		r,
		validatorDuty,
		attestationData,
		validatorDuty.DutySlot(),
		spectypes.DomainAttester,
	)
	if err != nil {
		return false, partialMsg, traces.Errorf(span, "failed signing attestation data: %w", err)
	}

	attDataRoot, err := attestationData.HashTreeRoot()
	if err != nil {
		return false, partialMsg, traces.Errorf(span, "failed to hash attestation data: %w", err)
	}

	const eventMsg = "signed attestation data"
	span.AddEvent(eventMsg, trace.WithAttributes(observability.BeaconBlockRootAttribute(attDataRoot)))
	logger.Debug(eventMsg,
		zap.Uint64("validator_index", uint64(validatorDuty.ValidatorIndex)),
		zap.String("pub_key", hex.EncodeToString(validatorDuty.PubKey[:])),
		zap.Any("attestation_data", attestationData),
		zap.String("attestation_data_root", hex.EncodeToString(attDataRoot[:])),
		zap.String("signing_root", hex.EncodeToString(partialMsg.SigningRoot[:])),
		zap.String("signature", hex.EncodeToString(partialMsg.PartialSignature[:])),
	)

	span.SetStatus(codes.Ok, "")

	return false, partialMsg, nil
}

// TODO finish edge case where some roots may be missing
func (r *AggregatorCommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_committee_post_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
			attribute.Int("ssv.validator.partial_signature_msg.count", len(signedMsg.Messages)),
		))
	defer span.End()

	span.AddEvent("base post consensus message processing")
	hasQuorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing post consensus message: %w", err)
	}

	logger = logger.With(fields.Slot(signedMsg.Slot))

	indices := make([]uint64, len(signedMsg.Messages))
	for i, msg := range signedMsg.Messages {
		indices[i] = uint64(msg.ValidatorIndex)
	}
	logger = logger.With(fields.ConsensusTime(r.measurements.ConsensusTime()))

	const eventMsg = "🧩 got partial signatures"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		zap.Bool("quorum", hasQuorum),
		fields.Slot(r.BaseRunner.State.StartingDuty.DutySlot()),
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Int("roots", len(roots)),
		zap.Uint64s("validators", indices))

	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("getting attestations, sync committees and root beacon objects")
	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := r.expectedPostConsensusRootsAndBeaconObjects(ctx, logger)
	if err != nil {
		return traces.Errorf(span, "could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		r.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDuties.Error())
		return ErrNoValidDuties
	}

	attestationsToSubmit := make(map[phase0.ValidatorIndex]*spec.VersionedAttestation)
	syncCommitteeMessagesToSubmit := make(map[phase0.ValidatorIndex]*altair.SyncCommitteeMessage)

	// Get unique roots to avoid repetition
	deduplicatedRoots := make(map[[32]byte]struct{})
	for _, root := range roots {
		deduplicatedRoots[root] = struct{}{}
	}

	var executionErr error

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(deduplicatedRoots)))
	// For each root that got at least one quorum, find the duties associated to it and try to submit
	for root := range deduplicatedRoots {
		// Get validators related to the given root
		role, validators, found := findValidators(root, attestationMap, committeeMap)

		if !found {
			// Edge case: since operators may have divergent sets of validators,
			// it's possible that an operator doesn't have the validator associated to a root.
			// In this case, we simply continue.
			continue
		}
		const eventMsg = "found validators for root"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconRoleAttribute(role),
			observability.BeaconBlockRootAttribute(root),
			observability.ValidatorCountAttribute(len(validators)),
		))
		logger.Debug(eventMsg,
			fields.Slot(r.BaseRunner.State.StartingDuty.DutySlot()),
			zap.String("role", role.String()),
			zap.String("root", hex.EncodeToString(root[:])),
			zap.Any("validators", validators),
		)

		type signatureResult struct {
			signature      phase0.BLSSignature
			validatorIndex phase0.ValidatorIndex
		}
		var (
			wg          sync.WaitGroup
			errCh       = make(chan error, len(validators))
			signatureCh = make(chan signatureResult, len(validators))
		)

		span.AddEvent("constructing sync-committee and attestations signature messages", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		for _, validator := range validators {
			// Skip if no quorum - We know that a root has quorum but not necessarily for the validator
			if !r.BaseRunner.State.PostConsensusContainer.HasQuorum(validator, root) {
				continue
			}
			// Skip if already submitted
			if r.HasSubmitted(role, validator) {
				continue
			}

			wg.Add(1)
			go func(validatorIndex phase0.ValidatorIndex, root [32]byte, roots map[[32]byte]struct{}) {
				defer wg.Done()

				share := r.BaseRunner.Share[validatorIndex]

				pubKey := share.ValidatorPubKey
				vlogger := logger.With(zap.Uint64("validator_index", uint64(validatorIndex)), zap.String("pubkey", hex.EncodeToString(pubKey[:])))

				sig, err := r.BaseRunner.State.ReconstructBeaconSig(r.BaseRunner.State.PostConsensusContainer, root, pubKey[:], validatorIndex)
				// If the reconstructed signature verification failed, fall back to verifying each partial signature
				if err != nil {
					for root := range roots {
						r.BaseRunner.FallBackAndVerifyEachSignature(r.BaseRunner.State.PostConsensusContainer, root, share.Committee, validatorIndex)
					}
					const eventMsg = "got post-consensus quorum but it has invalid signatures"
					span.AddEvent(eventMsg)
					vlogger.Error(eventMsg, fields.Slot(r.BaseRunner.State.StartingDuty.DutySlot()), zap.Error(err))

					errCh <- fmt.Errorf("%s: %w", eventMsg, err)
					return
				}

				vlogger.Debug("🧩 reconstructed partial signature")

				signatureCh <- signatureResult{
					validatorIndex: validatorIndex,
					signature:      (phase0.BLSSignature)(sig),
				}
			}(validator, root, deduplicatedRoots)
		}

		go func() {
			wg.Wait()
			close(signatureCh)
		}()

	listener:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				executionErr = err
			case signatureResult, ok := <-signatureCh:
				if !ok {
					break listener
				}

				validatorObjects, exists := beaconObjects[signatureResult.validatorIndex]
				if !exists {
					executionErr = fmt.Errorf("could not find beacon object for validator index: %d", signatureResult.validatorIndex)
					continue
				}
				sszObject, exists := validatorObjects[root]
				if !exists {
					executionErr = fmt.Errorf("could not find ssz object for root: %s", root)
					continue
				}

				// Store objects for multiple submission
				if role == spectypes.BNRoleSyncCommittee {
					syncMsg := sszObject.(*altair.SyncCommitteeMessage)
					syncMsg.Signature = signatureResult.signature

					syncCommitteeMessagesToSubmit[signatureResult.validatorIndex] = syncMsg
				} else if role == spectypes.BNRoleAttester {
					att := sszObject.(*spec.VersionedAttestation)
					att, err = specssv.VersionedAttestationWithSignature(att, signatureResult.signature)
					if err != nil {
						executionErr = fmt.Errorf("could not insert signature in versioned attestation")
						continue
					}

					attestationsToSubmit[signatureResult.validatorIndex] = att
				}
			}
		}

		logger.Debug("🧩 reconstructed partial signatures for root",
			zap.Uint64s("signers", getPostConsensusCommitteeSigners(r.BaseRunner.State, root)),
			fields.BlockRoot(root),
		)
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleCommittee)

	logger = logger.With(fields.PostConsensusTime(r.measurements.PostConsensusTime()))

	attestations := make([]*spec.VersionedAttestation, 0, len(attestationsToSubmit))
	for _, att := range attestationsToSubmit {
		if att != nil && att.ValidatorIndex != nil {
			attestations = append(attestations, att)
		}
	}

	r.measurements.EndDutyFlow()

	if len(attestations) > 0 {
		span.AddEvent("submitting attestations")
		submissionStart := time.Now()

		// Submit multiple attestations
		if err := r.beacon.SubmitAttestations(ctx, attestations); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleAttester)

			const errMsg = "could not submit attestations"
			logger.Error(errMsg, zap.Error(err))
			return traces.Errorf(span, "%s: %w", errMsg, err)
		}

		recordDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.BNRoleAttester, r.BaseRunner.State.RunningInstance.State.Round)

		attestationsCount := len(attestations)
		if attestationsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(
				ctx,
				uint32(attestationsCount),
				r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleAttester,
			)
		}

		attData, err := attestations[0].Data()
		if err != nil {
			return traces.Errorf(span, "could not get attestation data: %w", err)
		}
		const eventMsg = "✅ successfully submitted attestations"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconBlockRootAttribute(attData.BeaconBlockRoot),
			observability.DutyRoundAttribute(r.BaseRunner.State.RunningInstance.State.Round),
			observability.ValidatorCountAttribute(attestationsCount),
		))

		logger.Info(eventMsg,
			fields.Epoch(r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetBaseRunner().State.StartingDuty.DutySlot())),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(attData.BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(r.measurements.TotalDutyTime()),
			fields.Count(attestationsCount),
		)

		// Record successful submissions
		for validator := range attestationsToSubmit {
			r.RecordSubmission(spectypes.BNRoleAttester, validator)
		}
	}

	// Submit multiple sync committee
	syncCommitteeMessages := make([]*altair.SyncCommitteeMessage, 0, len(syncCommitteeMessagesToSubmit))
	for _, syncMsg := range syncCommitteeMessagesToSubmit {
		syncCommitteeMessages = append(syncCommitteeMessages, syncMsg)
	}

	if len(syncCommitteeMessages) > 0 {
		span.AddEvent("submitting sync committee")
		submissionStart := time.Now()
		if err := r.beacon.SubmitSyncMessages(ctx, syncCommitteeMessages); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleSyncCommittee)

			const errMsg = "could not submit sync committee messages"
			logger.Error(errMsg, zap.Error(err))
			return traces.Errorf(span, "%s: %w", errMsg, err)
		}

		recordDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.BNRoleSyncCommittee, r.BaseRunner.State.RunningInstance.State.Round)

		syncMsgsCount := len(syncCommitteeMessages)
		if syncMsgsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(
				ctx,
				uint32(syncMsgsCount),
				r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetBaseRunner().State.StartingDuty.DutySlot()),
				spectypes.BNRoleSyncCommittee,
			)
		}
		const eventMsg = "✅ successfully submitted sync committee"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconSlotAttribute(r.BaseRunner.State.StartingDuty.DutySlot()),
			observability.DutyRoundAttribute(r.BaseRunner.State.RunningInstance.State.Round),
			observability.BeaconBlockRootAttribute(syncCommitteeMessages[0].BeaconBlockRoot),
			observability.ValidatorCountAttribute(len(syncCommitteeMessages)),
			attribute.Float64("ssv.validator.duty.submission_time", time.Since(submissionStart).Seconds()),
			attribute.Float64("ssv.validator.duty.consensus_time_total", time.Since(r.measurements.consensusStart).Seconds()),
		))
		logger.Info(eventMsg,
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.BaseRunner.State.RunningInstance.State.Round),
			fields.BlockRoot(syncCommitteeMessages[0].BeaconBlockRoot),
			fields.SubmissionTime(time.Since(submissionStart)),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(r.measurements.TotalDutyTime()),
			fields.Count(syncMsgsCount),
		)

		// Record successful submissions
		for validator := range syncCommitteeMessagesToSubmit {
			r.RecordSubmission(spectypes.BNRoleSyncCommittee, validator)
		}
	}

	if executionErr != nil {
		span.SetStatus(codes.Error, executionErr.Error())
		return executionErr
	}

	// Check if duty has terminated (runner has submitted for all duties)
	if r.HasSubmittedAllValidatorDuties(attestationMap, committeeMap) {
		r.BaseRunner.State.Finished = true
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// HasSubmittedAllValidatorDuties -- Returns true if the runner has done submissions for all validators for the given slot
func (r *AggregatorCommitteeRunner) HasSubmittedAllValidatorDuties(attestationMap map[phase0.ValidatorIndex][32]byte, syncCommitteeMap map[phase0.ValidatorIndex][32]byte) bool {
	// Expected total
	expectedTotalSubmissions := len(attestationMap) + len(syncCommitteeMap)

	totalSubmissions := 0

	// Add submitted attestation duties
	for valIdx := range attestationMap {
		if r.HasSubmitted(spectypes.BNRoleAttester, valIdx) {
			totalSubmissions++
		}
	}
	// Add submitted sync committee duties
	for valIdx := range syncCommitteeMap {
		if r.HasSubmitted(spectypes.BNRoleSyncCommittee, valIdx) {
			totalSubmissions++
		}
	}
	return totalSubmissions >= expectedTotalSubmissions
}

// RecordSubmission -- Records a submission for the (role, validator index, slot) tuple
func (r *AggregatorCommitteeRunner) RecordSubmission(role spectypes.BeaconRole, validatorIndex phase0.ValidatorIndex, root [32]byte) {
	if _, ok := r.submittedDuties[role]; !ok {
		r.submittedDuties[role] = make(map[phase0.ValidatorIndex]map[[32]byte]struct{})
	}
	if _, ok := r.submittedDuties[role][validatorIndex]; !ok {
		r.submittedDuties[role][validatorIndex] = make(map[[32]byte]struct{})
	}
	r.submittedDuties[role][validatorIndex][root] = struct{}{}
}

// HasSubmitted -- Returns true if there is a record of submission for the (role, validator index, slot) tuple
func (r *AggregatorCommitteeRunner) HasSubmitted(role spectypes.BeaconRole, valIdx phase0.ValidatorIndex) bool {
	if _, ok := r.submittedDuties[role]; !ok {
		return false
	}
	_, ok := r.submittedDuties[role][valIdx]
	return ok
}

func findValidators(
	expectedRoot [32]byte,
	attestationMap map[phase0.ValidatorIndex][32]byte,
	committeeMap map[phase0.ValidatorIndex][32]byte) (spectypes.BeaconRole, []phase0.ValidatorIndex, bool) {
	var validators []phase0.ValidatorIndex

	// look for the expectedRoot in attestationMap
	for validator, root := range attestationMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleAttester, validators, true
	}
	// look for the expectedRoot in committeeMap
	for validator, root := range committeeMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleSyncCommittee, validators, true
	}
	return spectypes.BNRoleUnknown, nil, false
}

// Unneeded since no preconsensus phase
func (r *AggregatorCommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no pre consensus root for committee runner")
}

// This function signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (r *AggregatorCommitteeRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("unexpected expectedPostConsensusRootsAndDomain func call")
}

// expectedPreConsensusRoots returns the expected roots for the pre-consensus phase.
// It returns the aggregator and sync committee validator to root maps.
func (r *AggregatorCommitteeRunner) expectedPreConsensusRoots(ctx context.Context) (
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex]map[uint64][32]byte,
	error error,
) {
	aggregatorMap = make(map[phase0.ValidatorIndex][32]byte)
	contributionMap = make(map[phase0.ValidatorIndex]map[uint64][32]byte)

	duty := r.BaseRunner.State.StartingDuty.(*spectypes.AggregatorCommitteeDuty)

	for _, vDuty := range duty.ValidatorDuties {
		if vDuty == nil {
			continue
		}

		switch vDuty.Type {
		case spectypes.BNRoleAggregator:
			root, err := r.expectedAggregatorSelectionRoot(ctx, duty.Slot)
			if err != nil {
				continue
			}
			aggregatorMap[vDuty.ValidatorIndex] = root

		case spectypes.BNRoleSyncCommitteeContribution:
			if _, ok := contributionMap[vDuty.ValidatorIndex]; !ok {
				contributionMap[vDuty.ValidatorIndex] = make(map[uint64][32]byte)
			}

			for _, index := range vDuty.ValidatorSyncCommitteeIndices {
				root, err := r.expectedSyncCommitteeSelectionRoot(ctx, duty.Slot, index)
				if err != nil {
					continue
				}
				contributionMap[vDuty.ValidatorIndex][index] = root
			}

		default:
			return nil, nil, fmt.Errorf("invalid duty type in aggregator committee duty: %v", vDuty.Type)
		}
	}

	return aggregatorMap, contributionMap, nil
}

// expectedAggregatorSelectionRoot calculates the expected signing root for aggregator selection
func (r *AggregatorCommitteeRunner) expectedAggregatorSelectionRoot(
	ctx context.Context,
	slot phase0.Slot,
) ([32]byte, error) {
	epoch := r.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(slot)
	domain, err := r.beacon.DomainData(ctx, epoch, spectypes.DomainSelectionProof)
	if err != nil {
		return [32]byte{}, err
	}

	return spectypes.ComputeETHSigningRoot(spectypes.SSZUint64(slot), domain)
}

// expectedSyncCommitteeSelectionRoot calculates the expected signing root for sync committee selection
func (r *AggregatorCommitteeRunner) expectedSyncCommitteeSelectionRoot(
	ctx context.Context,
	slot phase0.Slot,
	syncCommitteeIndex uint64,
) ([32]byte, error) {
	subnet := r.beacon.SyncCommitteeSubnetID(phase0.CommitteeIndex(syncCommitteeIndex))

	data := &altair.SyncAggregatorSelectionData{
		Slot:              slot,
		SubcommitteeIndex: subnet,
	}

	epoch := r.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(slot)
	domain, err := r.beacon.DomainData(ctx, epoch, spectypes.DomainSyncCommitteeSelectionProof)
	if err != nil {
		return [32]byte{}, err
	}

	return spectypes.ComputeETHSigningRoot(data, domain)
}

func (r *AggregatorCommitteeRunner) expectedPostConsensusRootsAndBeaconObjects(ctx context.Context, logger *zap.Logger) (
	attestationMap map[phase0.ValidatorIndex][32]byte,
	syncCommitteeMap map[phase0.ValidatorIndex][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]interface{}, err error,
) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]interface{})
	duty := r.BaseRunner.State.StartingDuty
	// TODO DecidedValue should be interface??
	beaconVoteData := r.BaseRunner.State.DecidedValue
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(beaconVoteData); err != nil {
		return nil, nil, nil, fmt.Errorf("could not decode beacon vote: %w", err)
	}

	slot := duty.DutySlot()
	epoch := r.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(slot)

	dataVersion, _ := r.GetBaseRunner().NetworkConfig.ForkAtEpoch(epoch)

	for _, validatorDuty := range duty.(*spectypes.CommitteeDuty).ValidatorDuties {
		if validatorDuty == nil {
			continue
		}
		logger := logger.With(fields.Validator(validatorDuty.PubKey[:]))
		slot := validatorDuty.DutySlot()
		epoch := r.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(slot)
		switch validatorDuty.Type {
		case spectypes.BNRoleAttester:
			// Attestation object
			attestationData := constructAttestationData(beaconVote, validatorDuty, dataVersion)
			attestationResponse, err := specssv.ConstructVersionedAttestationWithoutSignature(attestationData, dataVersion, validatorDuty)
			if err != nil {
				logger.Debug("failed to construct attestation", zap.Error(err))
				continue
			}

			// Root
			domain, err := r.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainAttester)
			if err != nil {
				logger.Debug("failed to get attester domain", zap.Error(err))
				continue
			}

			root, err := spectypes.ComputeETHSigningRoot(attestationData, domain)
			if err != nil {
				logger.Debug("failed to compute attester root", zap.Error(err))
				continue
			}

			// Add to map
			attestationMap[validatorDuty.ValidatorIndex] = root
			if _, ok := beaconObjects[validatorDuty.ValidatorIndex]; !ok {
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]interface{})
			}
			beaconObjects[validatorDuty.ValidatorIndex][root] = attestationResponse
		case spectypes.BNRoleSyncCommittee:
			// Sync committee beacon object
			syncMsg := &altair.SyncCommitteeMessage{
				Slot:            slot,
				BeaconBlockRoot: beaconVote.BlockRoot,
				ValidatorIndex:  validatorDuty.ValidatorIndex,
			}

			// Root
			domain, err := r.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainSyncCommittee)
			if err != nil {
				logger.Debug("failed to get sync committee domain", zap.Error(err))
				continue
			}
			// Eth root
			blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
			root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
			if err != nil {
				logger.Debug("failed to compute sync committee root", zap.Error(err))
				continue
			}

			// Set root and beacon object
			syncCommitteeMap[validatorDuty.ValidatorIndex] = root
			if _, ok := beaconObjects[validatorDuty.ValidatorIndex]; !ok {
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]interface{})
			}
			beaconObjects[validatorDuty.ValidatorIndex][root] = syncMsg
		default:
			return nil, nil, nil, fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
		}
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

type preConsensusMetadata struct {
	ValidatorIndex     phase0.ValidatorIndex
	Role               spectypes.BeaconRole
	SyncCommitteeIndex uint64 // only for sync committee role
}

// findValidatorsForPreConsensusRoot finds all validators that have the given root in pre-consensus
func findValidatorsForPreConsensusRoot(
	expectedRoot [32]byte,
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex]map[uint64][32]byte,
) ([]preConsensusMetadata, bool) {
	var metadata []preConsensusMetadata

	// Check aggregator map
	for validator, root := range aggregatorMap {
		if root == expectedRoot {
			metadata = append(metadata, preConsensusMetadata{
				ValidatorIndex: validator,
				Role:           spectypes.BNRoleAggregator,
			})
		}
	}

	// Check sync committee contribution map
	for validator, indexMap := range contributionMap {
		for index, root := range indexMap {
			if root == expectedRoot {
				metadata = append(metadata, preConsensusMetadata{
					ValidatorIndex:     validator,
					Role:               spectypes.BNRoleSyncCommitteeContribution,
					SyncCommitteeIndex: index,
				})
			}
		}
	}

	return metadata, len(metadata) > 0
}

func (r *AggregatorCommitteeRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_aggregator_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	r.measurements.StartDutyFlow()
	r.measurements.StartPreConsensus()

	aggCommitteeDuty, ok := duty.(*spectypes.AggregatorCommitteeDuty)
	if !ok {
		return errors.New("invalid duty type for aggregator committee runner")
	}

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.AggregatorCommitteePartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	// Generate selection proofs for all validators and duties
	for _, vDuty := range aggCommitteeDuty.ValidatorDuties {
		//TODO(Aleg) decide if we need to keep this validation here
		if _, ok := r.BaseRunner.Share[vDuty.ValidatorIndex]; !ok {
			continue
		}

		switch vDuty.Type {
		case spectypes.BNRoleAggregator:
			span.AddEvent("signing beacon object")
			// Sign slot for aggregator selection proof
			partialSig, err := r.BaseRunner.signBeaconObject(
				ctx,
				r,
				vDuty,
				spectypes.SSZUint64(duty.DutySlot()),
				duty.DutySlot(),
				spectypes.DomainSelectionProof,
			)
			if err != nil {
				return traces.Errorf(span, "failed to sign aggregator selection proof: %w", err)
			}

			msg.Messages = append(msg.Messages, partialSig)

		case spectypes.BNRoleSyncCommitteeContribution:
			// Sign sync committee selection proofs for each subcommittee
			for _, index := range vDuty.ValidatorSyncCommitteeIndices {
				subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(index))

				data := &altair.SyncAggregatorSelectionData{
					Slot:              duty.DutySlot(),
					SubcommitteeIndex: subnet,
				}

				span.AddEvent("signing beacon object")
				partialSig, err := r.BaseRunner.signBeaconObject(
					ctx,
					r,
					vDuty,
					data,
					duty.DutySlot(),
					spectypes.DomainSyncCommitteeSelectionProof,
				)
				if err != nil {
					return traces.Errorf(span, "failed to sign sync committee selection proof: %w", err)
				}

				msg.Messages = append(msg.Messages, partialSig)
			}

		default:
			return traces.Error(span, fmt.Errorf("invalid validator duty type for aggregator committee: %v", vDuty.Type))
		}
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msg.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode aggregator committee partial signature message: %w", err)
	}

	r.measurements.StartConsensus()

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
		return traces.Errorf(span, "can't broadcast partial aggregator committee sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorCommitteeRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *AggregatorCommitteeRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}
