package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
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
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type AggregatorCommitteeRunner struct {
	BaseRunner     *BaseRunner
	network        specqbft.Network
	beacon         beacon.BeaconNode
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	// ValCheck is used to validate the qbft-value(s) proposed by other Operators.
	ValCheck ssv.ValueChecker

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
		ValCheck:        ssv.NewValidatorConsensusDataChecker(),
		beacon:          beacon,
		network:         network,
		signer:          signer,
		operatorSigner:  operatorSigner,
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
		valCheck       ssv.ValueChecker
	}

	// Create object and marshal
	alias := &CommitteeRunnerAlias{
		BaseRunner:     r.BaseRunner,
		beacon:         r.beacon,
		network:        r.network,
		signer:         r.signer,
		operatorSigner: r.operatorSigner,
		valCheck:       r.ValCheck,
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
		valCheck       ssv.ValueChecker
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
	r.ValCheck = aux.valCheck
	return nil
}
func (r *AggregatorCommitteeRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *AggregatorCommitteeRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *AggregatorCommitteeRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *AggregatorCommitteeRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *AggregatorCommitteeRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *AggregatorCommitteeRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *AggregatorCommitteeRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *AggregatorCommitteeRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
}

func (r *AggregatorCommitteeRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *AggregatorCommitteeRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *AggregatorCommitteeRunner) GetNetworkConfig() *networkconfig.Network {
	return r.BaseRunner.NetworkConfig
}

func (r *AggregatorCommitteeRunner) GetBeaconSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *AggregatorCommitteeRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AggregatorCommitteeRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
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
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_aggregator_selection_proof"),
		trace.WithAttributes(
		// TODO
		))
	defer span.End()

	isAggregator := r.beacon.IsAggregator(ctx, vDuty.Slot, vDuty.CommitteeIndex, vDuty.CommitteeLength, selectionProof[:])
	if !isAggregator {
		return false, nil
	}

	// TODO: waitToSlotTwoThirds(vDuty.Slot)

	attestation, _, err := r.beacon.GetAggregateAttestation(ctx, vDuty.Slot, vDuty.CommitteeIndex)
	if err != nil {
		return true, traces.Errorf(span, "failed to get aggregate attestation: %w", err)
	}

	aggregatorData.Aggregators = append(aggregatorData.Aggregators, spectypes.AssignedAggregator{
		ValidatorIndex: vDuty.ValidatorIndex,
		SelectionProof: selectionProof,
		CommitteeIndex: uint64(vDuty.CommitteeIndex),
	})

	// Marshal attestation for storage
	attestationBytes, err := attestation.MarshalSSZ()
	if err != nil {
		return true, traces.Errorf(span, "failed to marshal attestation: %w", err)
	}

	aggregatorData.AggregatorsCommitteeIndexes = append(aggregatorData.AggregatorsCommitteeIndexes, uint64(vDuty.CommitteeIndex))
	aggregatorData.Attestations = append(aggregatorData.Attestations, attestationBytes)

	return true, nil
}

// processSyncCommitteeSelectionProof handles sync committee selection proofs with known index
func (r *AggregatorCommitteeRunner) processSyncCommitteeSelectionProof(
	ctx context.Context,
	selectionProof phase0.BLSSignature,
	syncCommitteeIndex uint64,
	vDuty *spectypes.ValidatorDuty,
	aggregatorData *spectypes.AggregatorCommitteeConsensusData,
) (bool, error) {
	subnetID := r.beacon.SyncCommitteeSubnetID(phase0.CommitteeIndex(syncCommitteeIndex))

	isAggregator := r.beacon.IsSyncCommitteeAggregator(selectionProof[:])

	if !isAggregator {
		return false, nil // Not selected as sync committee aggregator
	}

	// Check if we already have a contribution for this sync committee subnet ID
	for _, existingSubnet := range aggregatorData.SyncCommitteeSubnets {
		if existingSubnet == subnetID {
			// Contribution already exists for this subnet—skip duplicate.
			return true, nil
		}
	}

	contributions, _, err := r.GetBeaconNode().GetSyncCommitteeContribution(
		ctx, vDuty.Slot, []phase0.BLSSignature{selectionProof}, []uint64{subnetID})
	if err != nil {
		return true, err
	}

	// Type assertion to get the actual Contributions object
	contribs, ok := contributions.(*spectypes.Contributions)
	if !ok {
		return true, errors.Errorf("unexpected contributions type: %T", contributions)
	}

	if len(*contribs) == 0 {
		return true, errors.New("no contributions found")
	}

	// Append the contribution(s)
	for _, contrib := range *contribs {
		if contrib.Contribution.SubcommitteeIndex != subnetID {
			continue
		}

		aggregatorData.Contributors = append(aggregatorData.Contributors, spectypes.AssignedAggregator{
			ValidatorIndex: vDuty.ValidatorIndex,
			SelectionProof: selectionProof,
		})

		aggregatorData.SyncCommitteeSubnets = append(aggregatorData.SyncCommitteeSubnets, subnetID)
		aggregatorData.SyncCommitteeContributions = append(aggregatorData.SyncCommitteeContributions, contrib.Contribution)
	}

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

	duty := r.BaseRunner.State.CurrentDuty.(*spectypes.AggregatorCommitteeDuty)
	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	dataVersion, _ := r.GetBaseRunner().NetworkConfig.ForkAtEpoch(epoch)
	aggregatorData := &spectypes.AggregatorCommitteeConsensusData{
		Version: dataVersion,
	}
	hasAnyAggregator := false

	rootSet := make(map[[32]byte]struct{})
	for _, root := range roots {
		rootSet[root] = struct{}{}
	}

	sortedRoots := make([][32]byte, 0, len(rootSet))
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
		metadataList, found := r.findValidatorsForPreConsensusRoot(root, aggregatorMap, contributionMap)
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
					isAggregator, err := r.processAggregatorSelectionProof(ctx, blsSig, vDuty, aggregatorData)
					if err == nil {
						if isAggregator {
							hasAnyAggregator = true
						}
					} else {
						anyErr = traces.Errorf(span, "failed to process aggregator selection proof: %w", err)
					}
				}

			case spectypes.BNRoleSyncCommitteeContribution:
				vDuty := r.findValidatorDuty(duty, validatorIndex, spectypes.BNRoleSyncCommitteeContribution)
				if vDuty != nil {
					isAggregator, err := r.processSyncCommitteeSelectionProof(ctx, blsSig, metadata.SyncCommitteeIndex, vDuty, aggregatorData)
					if err == nil {
						if isAggregator {
							hasAnyAggregator = true
						}
					} else {
						anyErr = traces.Errorf(span, "failed to process sync committee selection proof: %w", err)
					}
				}

			default:
				// This should never happen as we build rootToMetadata ourselves with valid roles
				return errors.Errorf("unexpected role type in pre-consensus metadata: %v", metadata.Role)
			}
		}
	}

	// Early exit if no aggregators selected
	if !hasAnyAggregator {
		r.BaseRunner.State.Finished = true
		if anyErr != nil {
			return anyErr
		}
		return nil
	}

	if err := aggregatorData.Validate(); err != nil {
		return traces.Errorf(span, "invalid aggregator consensus data: %w", err)
	}

	if err := r.BaseRunner.decide(ctx, logger, r, r.BaseRunner.State.CurrentDuty.DutySlot(), aggregatorData, r.ValCheck); err != nil {
		return traces.Errorf(span, "failed to start consensus")
	}

	if anyErr != nil {
		return anyErr
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
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, msg, &spectypes.BeaconVote{})
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
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleAggregatorCommittee)

	r.measurements.StartPostConsensus()

	duty := r.BaseRunner.State.CurrentDuty
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

					partialSigMsg, err := signBeaconObject(
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
		span.SetStatus(codes.Error, ErrNoValidDutiesToExecute.Error())
		return ErrNoValidDutiesToExecute
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
	partialMsg, err := signBeaconObject(
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
		fields.Slot(r.BaseRunner.State.CurrentDuty.DutySlot()),
		zap.Uint64("signer", signedMsg.Messages[0].Signer),
		zap.Int("roots", len(roots)),
		zap.Uint64s("validators", indices))

	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("getting aggregations, sync committee contributions and root beacon objects")
	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	aggregatorMap, contributionMap, beaconObjects, err := r.expectedPostConsensusRootsAndBeaconObjects(ctx, logger)
	if err != nil {
		return traces.Errorf(span, "could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		r.BaseRunner.State.Finished = true
		span.SetStatus(codes.Error, ErrNoValidDutiesToExecute.Error())
		return ErrNoValidDutiesToExecute
	}

	// Get unique roots to avoid repetition
	deduplicatedRoots := make(map[[32]byte]struct{})
	for _, root := range roots {
		deduplicatedRoots[root] = struct{}{}
	}

	sortedRoots := make([][32]byte, 0, len(deduplicatedRoots))
	for root := range deduplicatedRoots {
		sortedRoots = append(sortedRoots, root)
	}
	sort.Slice(sortedRoots, func(i, j int) bool {
		return bytes.Compare(sortedRoots[i][:], sortedRoots[j][:]) < 0
	})

	var executionErr error

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(deduplicatedRoots)))
	// For each root that got at least one quorum, find the duties associated to it and try to submit
	for _, root := range sortedRoots {
		// Get validators related to the given root
		role, validators, found := r.findValidatorsForPostConsensusRoot(root, aggregatorMap, contributionMap)

		if !found {
			// Edge case: operator doesn't have the validator associated to a root
			continue
		}
		const eventMsg = "found validators for root"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconRoleAttribute(role),
			observability.BeaconBlockRootAttribute(root),
			observability.ValidatorCountAttribute(len(validators)),
		))
		logger.Debug(eventMsg,
			fields.Slot(r.BaseRunner.State.CurrentDuty.DutySlot()),
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

		span.AddEvent("constructing sync committee contribution and aggregations signature messages", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
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
				if share == nil {
					return // TODO: make sure we handle this logic
				}

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
					vlogger.Error(eventMsg, fields.Slot(r.BaseRunner.State.CurrentDuty.DutySlot()), zap.Error(err))

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

				switch role {
				case spectypes.BNRoleAggregator:
					aggregateAndProof := sszObject.(*spec.VersionedAggregateAndProof)
					signedAgg, err := r.constructSignedAggregateAndProof(aggregateAndProof, signatureResult.signature)
					if err != nil {
						executionErr = fmt.Errorf("failed to construct signed aggregate and proof: %w", err)
						continue
					}

					// TODO: store in a map and submit afterwards like in committee duty?
					start := time.Now()
					if err := r.beacon.SubmitSignedAggregateSelectionProof(ctx, signedAgg); err != nil {
						executionErr = fmt.Errorf("failed to submit signed aggregate and proof: %w", err)
						continue
					}

					const eventMsg = "✅ successful submitted aggregate"
					span.AddEvent(eventMsg)
					logger.Debug(
						eventMsg,
						fields.SubmissionTime(time.Since(start)),
						fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
						fields.TotalDutyTime(r.measurements.TotalDutyTime()),
					)

					r.RecordSubmission(spectypes.BNRoleAggregator, signatureResult.validatorIndex, root)

				case spectypes.BNRoleSyncCommitteeContribution:
					contribAndProof := sszObject.(*altair.ContributionAndProof)
					signedContrib := &altair.SignedContributionAndProof{
						Message:   contribAndProof,
						Signature: signatureResult.signature,
					}

					// TODO: store in a map and submit afterwards like in committee duty?
					start := time.Now()
					if err := r.beacon.SubmitSignedContributionAndProof(ctx, signedContrib); err != nil {
						executionErr = fmt.Errorf("failed to submit signed contribution and proof: %w", err)
						continue
					}

					const eventMsg = "✅ successfully submitted sync committee aggregator"
					span.AddEvent(eventMsg)
					logger.Debug(
						eventMsg,
						fields.SubmissionTime(time.Since(start)),
						fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
						fields.TotalDutyTime(r.measurements.TotalDutyTime()),
					)

					r.RecordSubmission(spectypes.BNRoleSyncCommitteeContribution, signatureResult.validatorIndex, root)

				default:
					return errors.Errorf("unexpected role type in post-consensus: %v", role)
				}
			}
		}

		logger.Debug("🧩 reconstructed partial signatures for root",
			zap.Uint64s("signers", getPostConsensusCommitteeSigners(r.BaseRunner.State, root)),
			fields.BlockRoot(root),
		)
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleAggregatorCommittee)

	logger = logger.With(fields.PostConsensusTime(r.measurements.PostConsensusTime()))

	r.measurements.EndDutyFlow()

	if executionErr != nil {
		span.SetStatus(codes.Error, executionErr.Error())
		return executionErr
	}

	// Check if duty has terminated (runner has submitted for all duties)
	if r.HasSubmittedAllDuties() {
		r.BaseRunner.State.Finished = true
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorCommitteeRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, msg)
}

// HasSubmittedForValidator checks if a validator has submitted any duty for a given role
func (r *AggregatorCommitteeRunner) HasSubmittedForValidator(role spectypes.BeaconRole, validatorIndex phase0.ValidatorIndex) bool {
	if _, ok := r.submittedDuties[role]; !ok {
		return false
	}
	if _, ok := r.submittedDuties[role][validatorIndex]; !ok {
		return false
	}
	return len(r.submittedDuties[role][validatorIndex]) > 0
}

// HasSubmittedAllDuties checks if all expected duties have been submitted
func (r *AggregatorCommitteeRunner) HasSubmittedAllDuties() bool {
	duty := r.BaseRunner.State.CurrentDuty.(*spectypes.AggregatorCommitteeDuty)

	for _, vDuty := range duty.ValidatorDuties {
		if vDuty == nil {
			continue
		}

		if _, hasShare := r.BaseRunner.Share[vDuty.ValidatorIndex]; !hasShare {
			continue
		}

		if !r.HasSubmittedForValidator(vDuty.Type, vDuty.ValidatorIndex) {
			return false
		}
	}

	return true
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

// This function signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPreConsensusRoots that is called later
func (r *AggregatorCommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, fmt.Errorf("unexpected expectedPreConsensusRootsAndDomain func call, runner role %v", r.GetRole())
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
	err error,
) {
	aggregatorMap = make(map[phase0.ValidatorIndex][32]byte)
	contributionMap = make(map[phase0.ValidatorIndex]map[uint64][32]byte)

	duty := r.BaseRunner.State.CurrentDuty.(*spectypes.AggregatorCommitteeDuty)

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
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex][][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]interface{}, err error,
) {
	aggregatorMap = make(map[phase0.ValidatorIndex][32]byte)
	contributionMap = make(map[phase0.ValidatorIndex][][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]interface{})

	consensusData := &spectypes.AggregatorCommitteeConsensusData{}
	if err := consensusData.Decode(r.BaseRunner.State.DecidedValue); err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not decode consensus data")
	}

	epoch := r.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(r.BaseRunner.State.CurrentDuty.DutySlot())

	aggregateAndProofs, hashRoots, err := consensusData.GetAggregateAndProofs()
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not get aggregate and proofs")
	}

	for i, aggregateAndProof := range aggregateAndProofs {
		validatorIndex := consensusData.Aggregators[i].ValidatorIndex
		hashRoot := hashRoots[i]

		// Calculate signing root for aggregate and proof
		domain, err := r.beacon.DomainData(ctx, epoch, spectypes.DomainAggregateAndProof)
		if err != nil {
			continue
		}

		root, err := spectypes.ComputeETHSigningRoot(hashRoot, domain)
		if err != nil {
			continue
		}

		aggregatorMap[validatorIndex] = root

		// Store beacon object
		if _, ok := beaconObjects[validatorIndex]; !ok {
			beaconObjects[validatorIndex] = make(map[[32]byte]interface{})
		}
		beaconObjects[validatorIndex][root] = aggregateAndProof
	}

	contributions, err := consensusData.GetSyncCommitteeContributions()
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not get sync committee contributions")
	}
	for i, contribution := range contributions {
		validatorIndex := consensusData.Contributors[i].ValidatorIndex

		// Create contribution and proof
		contribAndProof := &altair.ContributionAndProof{
			AggregatorIndex: validatorIndex,
			Contribution:    &contribution.Contribution,
			SelectionProof:  consensusData.Contributors[i].SelectionProof,
		}

		// Calculate signing root
		domain, err := r.beacon.DomainData(ctx, epoch, spectypes.DomainContributionAndProof)
		if err != nil {
			continue
		}

		root, err := spectypes.ComputeETHSigningRoot(contribAndProof, domain)
		if err != nil {
			continue
		}

		contributionMap[validatorIndex] = append(contributionMap[validatorIndex], root)

		// Store beacon object
		if _, ok := beaconObjects[validatorIndex]; !ok {
			beaconObjects[validatorIndex] = make(map[[32]byte]interface{})
		}
		beaconObjects[validatorIndex][root] = contribAndProof
	}

	return aggregatorMap, contributionMap, beaconObjects, nil
}

type preConsensusMetadata struct {
	ValidatorIndex     phase0.ValidatorIndex
	Role               spectypes.BeaconRole
	SyncCommitteeIndex uint64 // only for sync committee role
}

// findValidatorsForPreConsensusRoot finds all validators that have the given root in pre-consensus
func (r *AggregatorCommitteeRunner) findValidatorsForPreConsensusRoot(
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

func (r *AggregatorCommitteeRunner) findValidatorsForPostConsensusRoot(
	expectedRoot [32]byte,
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex][][32]byte,
) (spectypes.BeaconRole, []phase0.ValidatorIndex, bool) {
	var validators []phase0.ValidatorIndex

	// Check aggregator map
	for validator, root := range aggregatorMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleAggregator, validators, true
	}

	// Check contribution map
	for validator, roots := range contributionMap {
		for _, root := range roots {
			if root == expectedRoot {
				validators = append(validators, validator)
				break
			}
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleSyncCommitteeContribution, validators, true
	}

	return spectypes.BNRoleUnknown, nil, false
}

// constructSignedAggregateAndProof constructs a signed aggregate and proof from versioned data
func (r *AggregatorCommitteeRunner) constructSignedAggregateAndProof(
	aggregateAndProof *spec.VersionedAggregateAndProof,
	signature phase0.BLSSignature,
) (*spec.VersionedSignedAggregateAndProof, error) {
	ret := &spec.VersionedSignedAggregateAndProof{
		Version: aggregateAndProof.Version,
	}

	switch ret.Version {
	case spec.DataVersionPhase0:
		ret.Phase0 = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Phase0,
			Signature: signature,
		}
	case spec.DataVersionAltair:
		ret.Altair = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Altair,
			Signature: signature,
		}
	case spec.DataVersionBellatrix:
		ret.Bellatrix = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Bellatrix,
			Signature: signature,
		}
	case spec.DataVersionCapella:
		ret.Capella = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Capella,
			Signature: signature,
		}
	case spec.DataVersionDeneb:
		ret.Deneb = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Deneb,
			Signature: signature,
		}
	case spec.DataVersionElectra:
		if aggregateAndProof.Electra == nil {
			return nil, errors.New("nil Electra aggregate and proof")
		}
		ret.Electra = &electra.SignedAggregateAndProof{
			Message:   aggregateAndProof.Electra,
			Signature: signature,
		}
	default:
		return nil, errors.Errorf("unknown version %s", ret.Version.String())
	}

	return ret, nil
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
			partialSig, err := signBeaconObject(
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
				partialSig, err := signBeaconObject(
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

				// TODO: find a better way to handle this
				if len(msg.Messages) == 0 || !bytes.Equal(msg.Messages[len(msg.Messages)-1].PartialSignature, partialSig.PartialSignature) {
					msg.Messages = append(msg.Messages, partialSig)
				}
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
