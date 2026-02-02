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
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// AggregatorCommitteeRunner has no DutyGuard because AggregatorCommitteeRunner's duties aren't slashable.
type AggregatorCommitteeRunner struct {
	BaseRunner     *BaseRunner
	network        specqbft.Network
	beacon         beacon.BeaconNode
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	// ValCheck is used to validate the qbft-value(s) proposed by other Operators.
	ValCheck ssv.ValueChecker

	measurements *dutyMeasurements

	// For aggregator role: tracks by validator index only (one submission per validator)
	// For sync committee contribution role: tracks by validator index and root (multiple submissions per validator)
	submittedDuties map[spectypes.BeaconRole]map[phase0.ValidatorIndex]map[[32]byte]struct{}
	// rootToSyncCommitteeIdx is the root->validator_sync_committee_index mapping for the current duty.
	rootToSyncCommitteeIdx map[phase0.Root]phase0.ValidatorIndex

	// IsAggregator is an exported struct field, so it can be mocked out for easy testing.
	IsAggregator func(
		ctx context.Context,
		slot phase0.Slot,
		committeeIndex phase0.CommitteeIndex,
		committeeLength uint64,
		slotSig []byte,
	) bool `json:"-"`
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
		ValCheck:        ssv.NewAggregatorCommitteeChecker(),
		beacon:          beacon,
		network:         network,
		signer:          signer,
		operatorSigner:  operatorSigner,
		submittedDuties: make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]map[[32]byte]struct{}),
		measurements:    newMeasurementsStore(),

		IsAggregator: beacon.IsAggregator,
	}, nil
}

func (r *AggregatorCommitteeRunner) StartNewDuty(
	ctx context.Context,
	logger *zap.Logger,
	duty spectypes.Duty,
	quorum uint64,
) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	d, ok := duty.(*spectypes.AggregatorCommitteeDuty)
	if !ok {
		return fmt.Errorf("duty is not an AggregatorCommitteeDuty: %T", duty)
	}

	span.SetAttributes(observability.DutyCountAttribute(len(d.ValidatorDuties)))
	err := r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
	if err != nil {
		return err
	}

	r.submittedDuties[spectypes.BNRoleAggregator] = make(map[phase0.ValidatorIndex]map[[32]byte]struct{})
	r.submittedDuties[spectypes.BNRoleSyncCommitteeContribution] = make(map[phase0.ValidatorIndex]map[[32]byte]struct{})

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
		return [32]byte{}, fmt.Errorf("could not encode AggregatorCommitteeRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (r *AggregatorCommitteeRunner) MarshalJSON() ([]byte, error) {
	type AggregatorCommitteeRunnerAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       ssv.ValueChecker
	}

	// Create object and marshal
	alias := &AggregatorCommitteeRunnerAlias{
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
	type AggregatorCommitteeRunnerAlias struct {
		BaseRunner     *BaseRunner
		beacon         beacon.BeaconNode
		network        specqbft.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       ssv.ValueChecker
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &AggregatorCommitteeRunnerAlias{}
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
func (r *AggregatorCommitteeRunner) findValidatorDuty(
	validatorIndex phase0.ValidatorIndex,
	role spectypes.BeaconRole,
) *spectypes.ValidatorDuty {
	duty := r.state().CurrentDuty.(*spectypes.AggregatorCommitteeDuty)

	for _, d := range duty.ValidatorDuties {
		if d.ValidatorIndex == validatorIndex && d.Type == role {
			return d
		}
	}

	return nil
}

// waitTwoThirdsIntoSlot waits until two-thirds of the slot has passed.
func (r *AggregatorCommitteeRunner) waitTwoThirdsIntoSlot(ctx context.Context, slot phase0.Slot) error {
	finalTime := r.GetNetworkConfig().SlotStartTime(slot).Add(2 * r.GetNetworkConfig().IntervalDuration())
	wait := time.Until(finalTime)
	if wait <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

// processSyncCommitteeSelectionProof handles sync committee selection proofs with known index
func (r *AggregatorCommitteeRunner) processSyncCommitteeSelectionProof(
	ctx context.Context,
	selectionProof phase0.BLSSignature,
	validatorSyncCommitteeIndex uint64,
	vDuty *spectypes.ValidatorDuty,
	aggregatorData *spectypes.AggregatorCommitteeConsensusData,
) (bool, error) {
	if !r.beacon.IsSyncCommitteeAggregator(selectionProof[:]) {
		return false, nil // Not selected as sync committee aggregator
	}

	subnetID := r.beacon.SyncCommitteeSubnetID(phase0.CommitteeIndex(validatorSyncCommitteeIndex))

	// Check if we already have a contribution for this sync committee subnet ID
	for _, contrib := range aggregatorData.SyncCommitteeContributions {
		if contrib.SubcommitteeIndex == subnetID {
			// If so, just add to contributors and return
			aggregatorData.Contributors = append(aggregatorData.Contributors, spectypes.AssignedAggregator{
				ValidatorIndex: vDuty.ValidatorIndex,
				SelectionProof: selectionProof,
				CommitteeIndex: subnetID,
			})
			return true, nil
		}
	}

	// Else, fetch contribution and include everything (if successful)
	contributions, _, err := r.GetBeaconNode().GetSyncCommitteeContribution(
		ctx,
		vDuty.Slot,
		[]phase0.BLSSignature{selectionProof},
		[]uint64{subnetID},
	)
	if err != nil {
		return true, fmt.Errorf("get sync committee contribution: %w", err)
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
			CommitteeIndex: subnetID,
		})

		aggregatorData.SyncCommitteeContributions = append(aggregatorData.SyncCommitteeContributions, contrib.Contribution)
	}

	return true, nil
}

func (r *AggregatorCommitteeRunner) ProcessPreConsensus(
	ctx context.Context,
	logger *zap.Logger,
	signedMsg *spectypes.PartialSignatureMessages,
) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if err != nil {
		return fmt.Errorf("failed processing selection proof message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		return nil
	}

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleAggregatorCommittee)

	aggregatorMap, contributionMap, err := r.expectedPreConsensusRoots(ctx)
	if err != nil {
		return fmt.Errorf("could not get expected pre-consensus roots: %w", err)
	}

	duty := r.state().CurrentDuty.(*spectypes.AggregatorCommitteeDuty)
	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	dataVersion, _ := r.GetBaseRunner().NetworkConfig.ForkAtEpoch(epoch)
	consensusData := &spectypes.AggregatorCommitteeConsensusData{
		Version: dataVersion,
	}
	hasAnyAggregator := false

	sort.Slice(roots, func(i, j int) bool {
		return bytes.Compare(roots[i][:], roots[j][:]) < 0
	})

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(roots)))

	type aggregatorSelection struct {
		duty           *spectypes.ValidatorDuty
		selectionProof phase0.BLSSignature
	}

	var aggregatorSelections []aggregatorSelection
	var anyErr error
	for i, root := range roots {
		metadataList, found := r.findValidatorsForPreConsensusRoot(root, aggregatorMap, contributionMap)
		if !found {
			// Edge case: since operators may have divergent sets of validators,
			// it's possible that an operator doesn't have the validator associated to a root.
			// In this case, we simply continue.
			continue
		}

		sort.Slice(metadataList, func(i, j int) bool {
			return metadataList[i].ValidatorIndex < metadataList[j].ValidatorIndex
		})

		for _, metadata := range metadataList {
			validatorIndex := metadata.ValidatorIndex
			share := r.BaseRunner.Share[validatorIndex]
			// Operators might have diverging views on which validators they have in a committee
			// (e.g., an operator might have not yet seen an ValidatorAdded event,
			// or failed to process it and moved on). Hence, we need to check for this explicitly every time.
			if share == nil {
				continue
			}
			pubKey := share.ValidatorPubKey

			// As per the comments below, the quorums (for root+validator pairs) we got from basePostConsensusMsgProcessing
			// call above are optimistic - some of these quorums might have been invalidated now, hence, to avoid an
			// unnecessary unsuccessful BLS signature reconstruction attempt we need to check if root+validator pair
			// still has quorum.
			gotQuorum, quorumSigners := r.state().PreConsensusContainer.HasQuorum(validatorIndex, root)
			// Explanation on why we need this check: https://github.com/ssvlabs/ssv/pull/2503#discussion_r2658112575
			if !gotQuorum {
				continue
			}

			vLogger := logger.With(
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.String("pubkey", hex.EncodeToString(pubKey[:])),
				fields.BlockRoot(root),
				zap.Uint64s("quorum_signers", quorumSigners),
			)

			// Reconstruct signature
			fullSig, err := r.state().ReconstructBeaconSig(
				r.state().PreConsensusContainer,
				root,
				share.ValidatorPubKey[:],
				validatorIndex,
			)
			if err != nil {
				// If the reconstructed signature verification failed, fall back to verifying each individual
				// partial signature + discarding the invalid ones. This should not happen often in practice,
				// but it's a very desirable optimization to have because when it does happen - we wouldn't
				// want to reconstruct lots of BLS signatures only to discover most of them being invalid.
				// Notes:
				// 1) FallBackAndVerifyEachSignature call may also lead to a certain root+validator pairs
				//    in PostConsensusContainer not having quorum anymore since it previously was computed
				//    optimistically.
				// 2) we need to verify partial signatures only for the roots we haven't tried reconstructing
				//    signatures for (hence roots[i:])
				// 3) since this code is running a bunch of concurrent go-routines, we need to be careful to
				//    not call FallBackAndVerifyEachSignature for the same root+validator pair multiple times -
				//    this is why we are parallelizing by validators only (and not by root+validator), processing
				//    each root sequentially
				for _, root := range roots[i:] {
					r.BaseRunner.FallBackAndVerifyEachSignature(
						r.state().PreConsensusContainer,
						root,
						share.Committee,
						validatorIndex,
					)
				}

				const eventMsg = "got pre-consensus quorum but it has invalid signatures"
				span.AddEvent(eventMsg)
				vLogger.Error(eventMsg, zap.Error(err))

				anyErr = err
				continue
			}

			var blsSig phase0.BLSSignature
			copy(blsSig[:], fullSig)

			switch metadata.Role {
			case spectypes.BNRoleAggregator:
				vDuty := r.findValidatorDuty(validatorIndex, spectypes.BNRoleAggregator)
				if vDuty != nil {
					if r.IsAggregator(ctx, vDuty.Slot, vDuty.CommitteeIndex, vDuty.CommitteeLength, blsSig[:]) {
						hasAnyAggregator = true
						aggregatorSelections = append(aggregatorSelections, aggregatorSelection{
							duty:           vDuty,
							selectionProof: blsSig,
						})
					}
				}

			case spectypes.BNRoleSyncCommitteeContribution:
				vDuty := r.findValidatorDuty(validatorIndex, spectypes.BNRoleSyncCommitteeContribution)
				if vDuty != nil {
					vIdx, ok := r.rootToSyncCommitteeIdx[root]
					if !ok {
						logger.Warn("root got a quorum, but is unknown to us", fields.Root(root))
						continue
					}

					isAggregator, err := r.processSyncCommitteeSelectionProof(
						ctx,
						blsSig,
						uint64(vIdx),
						vDuty,
						consensusData,
					)
					if err == nil {
						if isAggregator {
							hasAnyAggregator = true
						}
					} else {
						anyErr = fmt.Errorf("failed to process sync committee selection proof: %w", err)
					}
				}

			default:
				// This should never happen as we build rootToMetadata ourselves with valid roles
				return errors.Errorf("unexpected role type in pre-consensus metadata: %v", metadata.Role)
			}
		}
	}

	// Early exit if no error and no aggregators is selected (really no validator is aggregator or sync committee contributor)
	if !hasAnyAggregator && anyErr == nil {
		r.state().Finished = true
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.RoleAggregatorCommittee, 0)

		return nil
	}

	if len(aggregatorSelections) > 0 {
		// Wait once per duty before fetching aggregate attestations (spec: 2/3 into slot).
		if err := r.waitTwoThirdsIntoSlot(ctx, duty.DutySlot()); err != nil {
			return err
		}

	selectionLoop:
		for _, selection := range aggregatorSelections {
			// Check if attestation for committee index was already included
			for _, idx := range consensusData.AggregatorsCommitteeIndexes {
				if idx == uint64(selection.duty.CommitteeIndex) {
					// If so, just add to aggregators and return
					consensusData.Aggregators = append(consensusData.Aggregators, spectypes.AssignedAggregator{
						ValidatorIndex: selection.duty.ValidatorIndex,
						SelectionProof: selection.selectionProof,
						CommitteeIndex: uint64(selection.duty.CommitteeIndex),
					})
					continue selectionLoop
				}
			}

			// Else, fetch attestation and include everything (if successful)
			attestation, _, err := r.beacon.GetAggregateAttestation(ctx, selection.duty.Slot, selection.duty.CommitteeIndex)
			if err != nil {
				anyErr = fmt.Errorf("failed to get aggregate attestation: %w", err)
				continue
			}

			attestationBytes, err := attestation.MarshalSSZ()
			if err != nil {
				anyErr = fmt.Errorf("failed to marshal attestation: %w", err)
				continue
			}

			consensusData.Aggregators = append(consensusData.Aggregators, spectypes.AssignedAggregator{
				ValidatorIndex: selection.duty.ValidatorIndex,
				SelectionProof: selection.selectionProof,
				CommitteeIndex: uint64(selection.duty.CommitteeIndex),
			})
			consensusData.AggregatorsCommitteeIndexes = append(
				consensusData.AggregatorsCommitteeIndexes,
				uint64(selection.duty.CommitteeIndex),
			)
			consensusData.AggregatedAttestations = append(consensusData.AggregatedAttestations, attestationBytes)
		}
	}

	// If there was an error, and no aggregators or contributors were selected, return the error
	if len(consensusData.Aggregators) == 0 && len(consensusData.Contributors) == 0 && anyErr != nil {
		return anyErr
	}

	// Else, if some aggregators or contributors were selected (even with an error for others), proceed to consensus
	if err := consensusData.Validate(); err != nil {
		return fmt.Errorf("invalid aggregator committee consensus data: %w", err)
	}

	r.measurements.StartConsensus()
	if err := r.BaseRunner.decide(
		ctx,
		logger,
		r.state().CurrentDuty.DutySlot(),
		consensusData,
		r.ValCheck,
	); err != nil {
		return fmt.Errorf("failed to start consensus: %w", err)
	}

	// Raise error if any
	if anyErr != nil {
		return anyErr
	}

	return nil
}

func (r *AggregatorCommitteeRunner) ProcessConsensus(
	ctx context.Context,
	logger *zap.Logger,
	msg *spectypes.SignedSSVMessage,
) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("checking if instance is decided")
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(
		ctx,
		logger,
		r.ValCheck.CheckValue,
		msg,
		&spectypes.AggregatorCommitteeConsensusData{},
	)
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleAggregatorCommittee)

	duty := r.state().CurrentDuty
	aggCommDuty, ok := duty.(*spectypes.AggregatorCommitteeDuty)
	if !ok {
		return fmt.Errorf("duty is not an AggregatorCommitteeDuty: %T", duty)
	}

	consensusData := decidedValue.(*spectypes.AggregatorCommitteeConsensusData)

	aggProofs, err := consensusData.GetAggregateAndProofs()
	if err != nil {
		return fmt.Errorf("failed to get aggregate and proofs: %w", err)
	}

	messages := make([]*spectypes.PartialSignatureMessage, 0)
	for i, aggProof := range aggProofs {
		validatorIndex := consensusData.Aggregators[i].ValidatorIndex

		_, exists := r.BaseRunner.Share[validatorIndex]
		// Operators might have diverging views on which validators they have in a committee
		// (e.g., an operator might have not yet seen an ValidatorAdded event,
		// or failed to process it and moved on). Hence, we need to check for this explicitly every time.
		if !exists {
			continue
		}

		vDuty := r.findValidatorDuty(validatorIndex, spectypes.BNRoleAggregator)
		if vDuty == nil {
			continue
		}

		// Sign the aggregate and proof
		hashRoot, err := spectypes.GetAggregateAndProofHashRoot(aggProof)
		if err != nil {
			return errors.Wrap(err, "failed to get aggregate and proof hash root")
		}

		msg, err := signBeaconObject(
			ctx,
			r, vDuty, hashRoot,
			aggCommDuty.DutySlot(),
			spectypes.DomainAggregateAndProof,
		)
		if err != nil {
			return fmt.Errorf("failed to sign aggregate and proof: %w", err)
		}

		messages = append(messages, msg)
	}

	contributions, err := consensusData.GetSyncCommitteeContributions()
	if err != nil {
		return fmt.Errorf("failed to get sync committee contributions: %w", err)
	}

	for i, contribution := range contributions {
		validatorIndex := consensusData.Contributors[i].ValidatorIndex

		_, exists := r.BaseRunner.Share[validatorIndex]
		// Operators might have diverging views on which validators they have in a committee
		// (e.g., an operator might have not yet seen an ValidatorAdded event,
		// or failed to process it and moved on). Hence, we need to check for this explicitly every time.
		if !exists {
			continue
		}

		vDuty := r.findValidatorDuty(validatorIndex, spectypes.BNRoleSyncCommitteeContribution)
		if vDuty == nil {
			continue
		}

		contribAndProof := &altair.ContributionAndProof{
			AggregatorIndex: validatorIndex,
			Contribution:    &contribution.Contribution,
			SelectionProof:  consensusData.Contributors[i].SelectionProof,
		}

		// Sign the contribution and proof
		msg, err := signBeaconObject(
			ctx,
			r, vDuty, contribAndProof,
			aggCommDuty.DutySlot(),
			spectypes.DomainContributionAndProof,
		)
		if err != nil {
			return fmt.Errorf("failed to sign contribution and proof: %w", err)
		}

		messages = append(messages, msg)
	}

	if len(messages) == 0 {
		// Nothing to broadcast for this operator
		return nil
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     duty.DutySlot(),
		Messages: messages,
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
		return fmt.Errorf("failed to encode post consensus signature msg: %w", err)
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.BaseRunner.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	r.measurements.StartPostConsensus()
	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return fmt.Errorf("can't broadcast partial post consensus sig: %w", err)
	}

	return nil
}

func (r *AggregatorCommitteeRunner) ProcessPostConsensus(
	ctx context.Context,
	logger *zap.Logger,
	signedMsg *spectypes.PartialSignatureMessages,
) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("base post consensus message processing")
	hasQuorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}

	if !hasQuorum {
		return nil
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleAggregatorCommittee)

	span.AddEvent("getting aggregations, sync committee contributions and root beacon objects")
	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	aggregatorMap, contributionMap, beaconObjects, err := r.expectedPostConsensusRootsAndBeaconObjects(ctx)
	if err != nil {
		return fmt.Errorf("could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		return ErrNoValidDutiesToExecute
	}

	sort.Slice(roots, func(i, j int) bool {
		return bytes.Compare(roots[i][:], roots[j][:]) < 0
	})

	var executionErr error
	aggregatesToSubmit := make(map[phase0.ValidatorIndex]map[[32]byte]*spec.VersionedSignedAggregateAndProof)
	contributionsToSubmit := make(map[phase0.ValidatorIndex]map[[32]byte]*altair.SignedContributionAndProof)

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(roots)))
	// For each root that got at least one quorum, find the duties associated to it and try to submit
	for i, root := range roots {
		// Get validators related to the given root
		role, validators, found := r.findValidatorsForPostConsensusRoot(root, aggregatorMap, contributionMap)
		if !found {
			// Edge case: operator doesn't have the validator associated to a root. This probably might mean a bug.
			logger.Error("BUG: could not find validators for root",
				zap.String("root", hex.EncodeToString(root[:])),
			)
			continue
		}

		const eventMsg = "found validators for root"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconRoleAttribute(role),
			observability.BeaconBlockRootAttribute(root),
			observability.ValidatorCountAttribute(len(validators)),
		))
		logger.Debug(eventMsg,
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

		span.AddEvent("constructing sync committee contribution and aggregations signature messages",
			trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
		for _, validator := range validators {
			// As per the comments below, the quorums (for root+validator pairs) we got from basePostConsensusMsgProcessing
			// call above are optimistic - some of these quorums might have been invalidated now, hence, to avoid an
			// unnecessary unsuccessful BLS signature reconstruction attempt we need to check if root+validator pair
			// still has quorum.
			gotQuorum, quorumSigners := r.state().PostConsensusContainer.HasQuorum(validator, root)
			if !gotQuorum {
				continue
			}
			// Skip if already submitted
			if r.HasSubmitted(role, validator, root) {
				continue
			}

			wg.Add(1)
			go func(validatorIndex phase0.ValidatorIndex, root [32]byte) {
				defer wg.Done()

				share := r.BaseRunner.Share[validatorIndex]
				// Operators might have diverging views on which validators they have in a committee
				// (e.g., an operator might have not yet seen an ValidatorAdded event,
				// or failed to process it and moved on). Hence, we need to check for this explicitly every time.
				if share == nil {
					return
				}
				pubKey := share.ValidatorPubKey

				vlogger := logger.With(
					zap.Uint64("validator_index", uint64(validatorIndex)),
					zap.String("pubkey", hex.EncodeToString(pubKey[:])),
					fields.BlockRoot(root),
					zap.Uint64s("quorum_signers", quorumSigners),
				)

				sig, err := r.state().ReconstructBeaconSig(r.state().PostConsensusContainer, root, pubKey[:], validatorIndex)
				if err != nil {
					// If the reconstructed signature verification failed, fall back to verifying each individual
					// partial signature + discarding the invalid ones. This should not happen often in practice,
					// but it's a very desirable optimization to have because when it does happen - we wouldn't
					// want to reconstruct lots of BLS signatures only to discover most of them being invalid.
					// Notes:
					// 1) FallBackAndVerifyEachSignature call may also lead to a certain root+validator pairs
					//    in PostConsensusContainer not having quorum anymore since it previously was computed
					//    optimistically.
					// 2) we need to verify partial signatures only for the roots we haven't tried reconstructing
					//    signatures for (hence roots[i:])
					// 3) since this code is running a bunch of concurrent go-routines, we need to be careful to
					//    not call FallBackAndVerifyEachSignature for the same root+validator pair multiple times -
					//    this is why we are parallelizing by validators only (and not by root+validator), processing
					//    each root sequentially
					for _, root := range roots[i:] {
						r.BaseRunner.FallBackAndVerifyEachSignature(
							r.state().PostConsensusContainer,
							root,
							share.Committee,
							validatorIndex,
						)
					}
					const eventMsg = "got post-consensus quorum but it has invalid signatures"
					span.AddEvent(eventMsg)
					vlogger.Error(eventMsg, zap.Error(err))

					errCh <- spectypes.WrapError(
						spectypes.PostConsensusQuorumWithInvalidSignatures,
						fmt.Errorf("%s: %w", eventMsg, err),
					)
					return
				}

				vlogger.Debug("🧩 reconstructed partial signature")

				signatureCh <- signatureResult{
					validatorIndex: validatorIndex,
					signature:      (phase0.BLSSignature)(sig),
				}
			}(validator, root)
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
					executionErr = fmt.Errorf("could not find beacon object for validator index: %d",
						signatureResult.validatorIndex)
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

					if aggregatesToSubmit[signatureResult.validatorIndex] == nil {
						aggregatesToSubmit[signatureResult.validatorIndex] = make(map[[32]byte]*spec.VersionedSignedAggregateAndProof)
					}
					aggregatesToSubmit[signatureResult.validatorIndex][root] = signedAgg

				case spectypes.BNRoleSyncCommitteeContribution:
					contribAndProof := sszObject.(*altair.ContributionAndProof)
					signedContrib := &altair.SignedContributionAndProof{
						Message:   contribAndProof,
						Signature: signatureResult.signature,
					}

					if contributionsToSubmit[signatureResult.validatorIndex] == nil {
						contributionsToSubmit[signatureResult.validatorIndex] = make(map[[32]byte]*altair.SignedContributionAndProof)
					}
					contributionsToSubmit[signatureResult.validatorIndex][root] = signedContrib

				default:
					return errors.Errorf("unexpected role type in post-consensus: %v", role)
				}
			}
		}

		logger.Debug("🧩 reconstructed partial signatures for root",
			fields.BlockRoot(root),
		)
	}

	for validatorIndex, signedByRoot := range aggregatesToSubmit {
		for root, signedAgg := range signedByRoot {
			start := time.Now()
			if err := r.beacon.SubmitSignedAggregateSelectionProof(ctx, signedAgg); err != nil {
				recordFailedSubmission(ctx, spectypes.BNRoleAggregator)
				executionErr = fmt.Errorf("failed to submit signed aggregate and proof: %w", err)
				continue
			}

			const eventMsg = "✅ successfully submitted signed aggregate and proof"
			span.AddEvent(eventMsg)
			logger.Debug(
				eventMsg,
				fields.BlockRoot(root),
				fields.Took(time.Since(start)),
				fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
				fields.TotalDutyTime(r.measurements.TotalDutyTime()),
			)

			recordSuccessfulSubmission(
				ctx,
				1,
				r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.state().CurrentDuty.DutySlot()),
				spectypes.BNRoleAggregator,
			)

			r.RecordSubmission(spectypes.BNRoleAggregator, validatorIndex, root)
		}
	}

	for validatorIndex, signedByRoot := range contributionsToSubmit {
		for root, signedContrib := range signedByRoot {
			start := time.Now()
			if err := r.beacon.SubmitSignedContributionAndProof(ctx, signedContrib); err != nil {
				recordFailedSubmission(ctx, spectypes.BNRoleSyncCommitteeContribution)
				executionErr = fmt.Errorf("failed to submit signed contribution and proof: %w", err)
				continue
			}

			const eventMsg = "✅ successfully submitted sync committee contributions"
			span.AddEvent(eventMsg)
			logger.Debug(
				eventMsg,
				fields.BlockRoot(root),
				fields.Took(time.Since(start)),
				fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
				fields.TotalDutyTime(r.measurements.TotalDutyTime()),
			)

			recordSuccessfulSubmission(
				ctx,
				1,
				r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.state().CurrentDuty.DutySlot()),
				spectypes.BNRoleSyncCommitteeContribution,
			)

			r.RecordSubmission(spectypes.BNRoleSyncCommitteeContribution, validatorIndex, root)
		}
	}

	if executionErr != nil {
		return executionErr
	}

	// Check if duty has terminated (runner has submitted for all duties)
	if r.HasSubmittedAllDuties(ctx) {
		r.state().Finished = true
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.RoleAggregatorCommittee, r.state().RunningInstance.State.Round)
		const dutyFinishedEvent = "✔️finished duty processing (100% success)"
		logger.Info(dutyFinishedEvent,
			fields.ConsensusTime(r.measurements.ConsensusTime()),
			fields.ConsensusRounds(uint64(r.state().RunningInstance.State.Round)),
			fields.PostConsensusTime(r.measurements.PostConsensusTime()),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(r.measurements.TotalDutyTime()),
		)
		span.AddEvent(dutyFinishedEvent)
		return nil
	}
	const dutyFinishedEvent = "✔️finished duty processing (partial success)"
	logger.Info(dutyFinishedEvent,
		fields.ConsensusTime(r.measurements.ConsensusTime()),
		fields.ConsensusRounds(uint64(r.state().RunningInstance.State.Round)),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)
	span.AddEvent(dutyFinishedEvent)

	return nil
}

func (r *AggregatorCommitteeRunner) OnTimeoutQBFT(
	ctx context.Context,
	logger *zap.Logger,
	timeoutData *ssvtypes.TimeoutData,
) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, timeoutData)
}

// HasSubmittedAllDuties checks if all expected duties have been submitted.
// For aggregator role we expect exactly one submission per validator.
// For sync committee contribution role we expect one submission per expected root
// (i.e., per subcommittee index assigned to that validator for this slot).
func (r *AggregatorCommitteeRunner) HasSubmittedAllDuties(ctx context.Context) bool {
	duty := r.state().CurrentDuty.(*spectypes.AggregatorCommitteeDuty)

	// Build the expected post-consensus roots per validator/role from the decided data.
	aggregatorMap, contributionMap, _, err := r.expectedPostConsensusRootsAndBeaconObjects(ctx)
	if err != nil {
		// If we can't resolve the expected set, do not finish yet.
		return false
	}

	for _, vDuty := range duty.ValidatorDuties {
		if vDuty == nil {
			continue
		}

		// Only consider validators this operator actually runs.
		if _, hasShare := r.BaseRunner.Share[vDuty.ValidatorIndex]; !hasShare {
			continue
		}

		switch vDuty.Type {
		case spectypes.BNRoleAggregator:
			// Expect exactly one aggregate root for this validator.
			expectedRoot, ok := aggregatorMap[vDuty.ValidatorIndex]
			if !ok {
				// If consensus did not include this validator's aggregate, we haven't finished.
				return false
			}
			if !r.HasSubmitted(spectypes.BNRoleAggregator, vDuty.ValidatorIndex, expectedRoot) {
				return false
			}

		case spectypes.BNRoleSyncCommitteeContribution:
			// Expect a submission for every contribution root assigned to this validator.
			expectedRoots, ok := contributionMap[vDuty.ValidatorIndex]
			if !ok || len(expectedRoots) == 0 {
				// The duty indicates sync committee work but no expected roots were found.
				return false
			}
			for _, root := range expectedRoots {
				if !r.HasSubmitted(spectypes.BNRoleSyncCommitteeContribution, vDuty.ValidatorIndex, root) {
					return false
				}
			}

		default:
			// Unknown role type: don't allow finishing.
			return false
		}
	}

	return true
}

// RecordSubmission -- Records a submission for the (role, validator index, slot) tuple
func (r *AggregatorCommitteeRunner) RecordSubmission(
	role spectypes.BeaconRole,
	validatorIndex phase0.ValidatorIndex,
	root [32]byte,
) {
	if _, ok := r.submittedDuties[role]; !ok {
		r.submittedDuties[role] = make(map[phase0.ValidatorIndex]map[[32]byte]struct{})
	}
	if _, ok := r.submittedDuties[role][validatorIndex]; !ok {
		r.submittedDuties[role][validatorIndex] = make(map[[32]byte]struct{})
	}
	r.submittedDuties[role][validatorIndex][root] = struct{}{}
}

// HasSubmitted -- Returns true if there is a record of submission for the (role, validator index, slot) tuple
func (r *AggregatorCommitteeRunner) HasSubmitted(
	role spectypes.BeaconRole,
	validatorIndex phase0.ValidatorIndex,
	root [32]byte,
) bool {
	if _, ok := r.submittedDuties[role]; !ok {
		return false
	}
	if _, ok := r.submittedDuties[role][validatorIndex]; !ok {
		return false
	}
	_, submitted := r.submittedDuties[role][validatorIndex][root]
	return submitted
}

// This function signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPreConsensusRoots that is called later
func (r *AggregatorCommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError,
		fmt.Errorf("unexpected expectedPreConsensusRootsAndDomain func call, runner role %v", r.GetRole())
}

// This function signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (r *AggregatorCommitteeRunner) expectedPostConsensusRootsAndDomain(context.Context) (
	[]ssz.HashRoot,
	phase0.DomainType,
	error,
) {
	return nil, spectypes.DomainError, errors.New("unexpected expectedPostConsensusRootsAndDomain func call")
}

// expectedPreConsensusRoots returns the expected roots for the pre-consensus phase.
// It returns the aggregator and sync committee validator to root maps.
func (r *AggregatorCommitteeRunner) expectedPreConsensusRoots(ctx context.Context) (
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex]map[ValidatorSyncCommitteeIndex][32]byte,
	err error,
) {
	aggregatorMap = make(map[phase0.ValidatorIndex][32]byte)
	contributionMap = make(map[phase0.ValidatorIndex]map[ValidatorSyncCommitteeIndex][32]byte)

	duty := r.state().CurrentDuty.(*spectypes.AggregatorCommitteeDuty)

	for _, vDuty := range duty.ValidatorDuties {
		if vDuty == nil {
			continue
		}

		switch vDuty.Type {
		case spectypes.BNRoleAggregator:
			root, err := r.expectedAggregatorSelectionRoot(ctx, duty.Slot)
			if err != nil {
				logger.Debug("failed to compute aggregator selection root",
					zap.Uint64("validator_index", uint64(vDuty.ValidatorIndex)),
					zap.Error(err),
				)
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
					logger.Debug("failed to compute sync committee selection root",
						zap.Uint64("validator_index", uint64(vDuty.ValidatorIndex)),
						zap.Uint64("subcommittee_index", index),
						zap.Error(err),
					)
					continue
				}
				contributionMap[vDuty.ValidatorIndex][index] = root
			}

		default:
			return nil, nil,
				fmt.Errorf("invalid duty type in aggregator committee duty: %v", vDuty.Type)
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
	validatorSyncCommitteeIndex uint64,
) ([32]byte, error) {
	subnet := r.beacon.SyncCommitteeSubnetID(phase0.CommitteeIndex(validatorSyncCommitteeIndex))

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

func (r *AggregatorCommitteeRunner) expectedPostConsensusRootsAndBeaconObjects(ctx context.Context) (
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex][][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]interface{}, err error,
) {
	aggregatorMap = make(map[phase0.ValidatorIndex][32]byte)
	contributionMap = make(map[phase0.ValidatorIndex][][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]interface{})

	consensusData := &spectypes.AggregatorCommitteeConsensusData{}
	if err := consensusData.Decode(r.state().DecidedValue); err != nil {
		return nil, nil, nil,
			errors.Wrap(err, "could not decode consensus data")
	}

	epoch := r.GetBaseRunner().NetworkConfig.EstimatedEpochAtSlot(r.state().CurrentDuty.DutySlot())

	aggregateAndProofs, err := consensusData.GetAggregateAndProofs()
	if err != nil {
		return nil, nil, nil,
			errors.Wrap(err, "could not get aggregate and proofs")
	}

	for i, aggregateAndProof := range aggregateAndProofs {
		validatorIndex := consensusData.Aggregators[i].ValidatorIndex
		hashRoot, err := spectypes.GetAggregateAndProofHashRoot(aggregateAndProof)
		if err != nil {
			logger.Debug("failed to compute aggregate and proof hash root",
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.Error(err),
			)
			continue
		}

		// Calculate signing root for aggregate and proof
		domain, err := r.beacon.DomainData(ctx, epoch, spectypes.DomainAggregateAndProof)
		if err != nil {
			logger.Debug("failed to get aggregate and proof domain",
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.Error(err),
			)
			continue
		}

		root, err := spectypes.ComputeETHSigningRoot(hashRoot, domain)
		if err != nil {
			logger.Debug("failed to compute aggregate and proof signing root",
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.Error(err),
			)
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
		return nil, nil, nil,
			errors.Wrap(err, "could not get sync committee contributions")
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
			logger.Debug("failed to get contribution and proof domain",
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.Uint64("subcommittee_index", contribution.Contribution.SubcommitteeIndex),
				zap.Error(err),
			)
			continue
		}

		root, err := spectypes.ComputeETHSigningRoot(contribAndProof, domain)
		if err != nil {
			logger.Debug("failed to compute contribution and proof signing root",
				zap.Uint64("validator_index", uint64(validatorIndex)),
				zap.Uint64("subcommittee_index", contribution.Contribution.SubcommitteeIndex),
				zap.Error(err),
			)
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

// ValidatorSyncCommitteeIndex is the index of the validator in the list of sync committee participants.
// The SubnetID (or SubcommitteeIndex) can be computed as ValidatorSyncCommitteeIndex // (SYNC_COMMITTEE_SIZE/ SYNC_COMMITTEE_SUBNET_COUNT)
type ValidatorSyncCommitteeIndex = uint64

type preConsensusMetadata struct {
	ValidatorIndex              phase0.ValidatorIndex
	Role                        spectypes.BeaconRole
	ValidatorSyncCommitteeIndex ValidatorSyncCommitteeIndex // only for sync committee role
}

// findValidatorsForPreConsensusRoot finds all validators that have the given root in pre-consensus
func (r *AggregatorCommitteeRunner) findValidatorsForPreConsensusRoot(
	expectedRoot [32]byte,
	aggregatorMap map[phase0.ValidatorIndex][32]byte,
	contributionMap map[phase0.ValidatorIndex]map[ValidatorSyncCommitteeIndex][32]byte,
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
					ValidatorIndex:              validator,
					Role:                        spectypes.BNRoleSyncCommitteeContribution,
					ValidatorSyncCommitteeIndex: index,
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
	case spec.DataVersionFulu:
		if aggregateAndProof.Fulu == nil {
			return nil, errors.New("nil Fulu aggregate and proof")
		}
		ret.Fulu = &electra.SignedAggregateAndProof{
			Message:   aggregateAndProof.Fulu,
			Signature: signature,
		}
	default:
		return nil, errors.Errorf("unknown version %s", ret.Version.String())
	}

	return ret, nil
}

// ValidateAggregatorCommitteeDuty checks that:
// - all slots values are equal
// - BeaconRole is either BNRoleAggregator or BNRoleSyncCommitteeContribution
// - Validator indexes exist in the provided map
// TODO: use (*AggregatorCommitteeDuty).Validate from spec after fork
func ValidateAggregatorCommitteeDuty(
	acd *spectypes.AggregatorCommitteeDuty,
	validatorIndex map[phase0.ValidatorIndex]struct{},
) error {
	const InvalidAggregatorCommitteeDutyErrorCode = 82

	slot := acd.Slot
	for _, vd := range acd.ValidatorDuties {
		if vd.Slot != slot {
			return spectypes.NewError(InvalidAggregatorCommitteeDutyErrorCode, "mismatched slot in validator duty")
		}
		if vd.Type != spectypes.BNRoleAggregator && vd.Type != spectypes.BNRoleSyncCommitteeContribution {
			return spectypes.NewError(InvalidAggregatorCommitteeDutyErrorCode, "invalid beacon role in validator duty")
		}
		if _, ok := validatorIndex[vd.ValidatorIndex]; !ok {
			return spectypes.NewError(InvalidAggregatorCommitteeDutyErrorCode, "validator index not found in duty")
		}
	}

	return nil
}

func (r *AggregatorCommitteeRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	span := trace.SpanFromContext(ctx)

	r.measurements.StartDutyFlow()

	aggCommitteeDuty, ok := duty.(*spectypes.AggregatorCommitteeDuty)
	if !ok {
		return errors.New("invalid duty type for aggregator committee runner")
	}

	// Validate duty
	valIdxs := make(map[phase0.ValidatorIndex]struct{})
	for idx := range r.BaseRunner.Share {
		valIdxs[idx] = struct{}{}
	}
	if err := ValidateAggregatorCommitteeDuty(aggCommitteeDuty, valIdxs); err != nil {
		return err
	}

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.AggregatorCommitteePartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	r.rootToSyncCommitteeIdx = make(map[phase0.Root]phase0.ValidatorIndex)

	// Generate selection proofs for all validators and duties
	for _, vDuty := range aggCommitteeDuty.ValidatorDuties {
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
				return fmt.Errorf("failed to sign aggregator selection proof: %w", err)
			}

			msg.Messages = append(msg.Messages, partialSig)

		case spectypes.BNRoleSyncCommitteeContribution:
			// Sign sync committee selection proofs for each subcommittee
			// Selection proof depends only on slot+subcommittee index, so emit at most one per subnet.
			seenSubnets := make(map[uint64]struct{})
			for _, index := range vDuty.ValidatorSyncCommitteeIndices {
				subnet := r.GetBeaconNode().SyncCommitteeSubnetID(phase0.CommitteeIndex(index))
				if _, seen := seenSubnets[subnet]; seen {
					continue
				}
				seenSubnets[subnet] = struct{}{}

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
					return fmt.Errorf("failed to sign sync committee selection proof: %w", err)
				}

				msg.Messages = append(msg.Messages, partialSig)
				r.rootToSyncCommitteeIdx[partialSig.SigningRoot] = phase0.ValidatorIndex(index)
			}

		default:
			return fmt.Errorf("invalid validator duty type for aggregator committee: %v", vDuty.Type)
		}
	}

	// Early exit if no selection proofs needed
	if len(msg.Messages) == 0 {
		r.state().Finished = true
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.RoleAggregatorCommittee, 0)
		const dutyFinishedNoMessages = "✔️successfully finished duty processing (no messages)"
		logger.Info(dutyFinishedNoMessages,
			fields.PreConsensusTime(r.measurements.PreConsensusTime()),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(r.measurements.TotalDutyTime()),
		)
		span.AddEvent(dutyFinishedNoMessages)
		return nil
	}

	msgID := spectypes.NewMsgID(
		r.BaseRunner.NetworkConfig.DomainType,
		r.GetBaseRunner().QBFTController.CommitteeMember.CommitteeID[:],
		r.BaseRunner.RunnerRoleType,
	)
	encodedMsg, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("could not encode aggregator committee partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	r.measurements.StartPreConsensus()
	span.AddEvent("broadcasting signed SSV message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return fmt.Errorf("can't broadcast partial aggregator committee sig: %w", err)
	}

	return nil
}

func (r *AggregatorCommitteeRunner) state() *State {
	return r.BaseRunner.State
}

func (r *AggregatorCommitteeRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *AggregatorCommitteeRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}
