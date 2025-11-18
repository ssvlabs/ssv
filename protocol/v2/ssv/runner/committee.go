package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
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

type CommitteeDutyGuard interface {
	StartDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error
	ValidDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error
}

type CommitteeRunner struct {
	BaseRunner *BaseRunner

	// attestingValidators is a list of validator this committee-runner will be processing attestation duties for.
	attestingValidators []phase0.BLSPubKey

	network             specqbft.Network
	beacon              beacon.BeaconNode
	signer              ekm.BeaconSigner
	operatorSigner      ssvtypes.OperatorSigner
	DutyGuard           CommitteeDutyGuard
	doppelgangerHandler DoppelgangerProvider
	measurements        *dutyMeasurements

	// ValCheck is used to validate the qbft-value(s) proposed by other Operators.
	ValCheck ssv.ValueChecker

	submittedDuties map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}
}

func NewCommitteeRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	attestingValidators []phase0.BLSPubKey,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	dutyGuard CommitteeDutyGuard,
	doppelgangerHandler DoppelgangerProvider,
) (Runner, error) {
	if len(share) == 0 {
		return nil, errors.New("no shares")
	}
	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleCommittee,
			NetworkConfig:  networkConfig,
			Share:          share,
			QBFTController: qbftController,
		},

		attestingValidators: attestingValidators,

		beacon:              beacon,
		network:             network,
		signer:              signer,
		operatorSigner:      operatorSigner,
		submittedDuties:     make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}),
		DutyGuard:           dutyGuard,
		doppelgangerHandler: doppelgangerHandler,
		measurements:        newMeasurementsStore(),
	}, nil
}

func (r *CommitteeRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	d, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		return fmt.Errorf("duty is not a CommitteeDuty: %T", duty)
	}

	span.SetAttributes(observability.DutyCountAttribute(len(d.ValidatorDuties)))

	for _, validatorDuty := range d.ValidatorDuties {
		err := r.DutyGuard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), d.DutySlot())
		if err != nil {
			return fmt.Errorf(
				"could not start %s duty at slot %d for validator %x: %w",
				validatorDuty.Type, d.DutySlot(), validatorDuty.PubKey, err,
			)
		}
	}
	err := r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
	if err != nil {
		return err
	}

	r.submittedDuties[spectypes.BNRoleAttester] = make(map[phase0.ValidatorIndex]struct{})
	r.submittedDuties[spectypes.BNRoleSyncCommittee] = make(map[phase0.ValidatorIndex]struct{})

	return nil
}

func (r *CommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *CommitteeRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

func (r *CommitteeRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode CommitteeRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (r *CommitteeRunner) MarshalJSON() ([]byte, error) {
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

func (r *CommitteeRunner) UnmarshalJSON(data []byte) error {
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

func (r *CommitteeRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *CommitteeRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *CommitteeRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *CommitteeRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *CommitteeRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *CommitteeRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *CommitteeRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *CommitteeRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
}

func (r *CommitteeRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *CommitteeRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *CommitteeRunner) GetNetworkConfig() *networkconfig.Network {
	return r.BaseRunner.NetworkConfig
}

func (r *CommitteeRunner) GetBeaconSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *CommitteeRunner) state() *State {
	return r.BaseRunner.State
}

func (r *CommitteeRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *CommitteeRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre consensus phase for committee runner")
}

func (r *CommitteeRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("processing QBFT consensus msg")
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, msg, &spectypes.BeaconVote{})
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleCommittee)

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
		return fmt.Errorf("duty is not a CommitteeDuty: %T", duty)
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
				if err := r.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
					const eventMsg = "duty is no longer valid"
					span.AddEvent(eventMsg, trace.WithAttributes(
						observability.ValidatorIndexAttribute(validatorDuty.ValidatorIndex),
						observability.ValidatorPublicKeyAttribute(validatorDuty.PubKey),
						observability.BeaconRoleAttribute(validatorDuty.Type),
					))
					logger.Warn(eventMsg, fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
					continue
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
			return err
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

		return nil
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID: spectypes.NewMsgID(
			r.BaseRunner.NetworkConfig.DomainType,
			r.BaseRunner.QBFTController.CommitteeMember.CommitteeID[:],
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
	if err := r.GetNetwork().Broadcast(ssvMsg.MsgID, msgToBroadcast); err != nil {
		return fmt.Errorf("can't broadcast partial post consensus sig: %w", err)
	}
	const broadcastedPostConsensusMsgEvent = "broadcasted post-consensus partial signature message"
	logger.Debug(broadcastedPostConsensusMsgEvent)
	span.AddEvent(broadcastedPostConsensusMsgEvent)

	return nil
}

func (r *CommitteeRunner) signAttesterDuty(
	ctx context.Context,
	validatorDuty *spectypes.ValidatorDuty,
	beaconVote *spectypes.BeaconVote,
	version spec.DataVersion,
	logger *zap.Logger) (isBlocked bool, partialSig *spectypes.PartialSignatureMessage, err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("doppelganger: checking if signing is allowed")
	// Doppelganger protection applies only to attester duties since they are slashable.
	// Sync committee duties are not slashable, so they are always allowed.
	if !r.doppelgangerHandler.CanSign(validatorDuty.ValidatorIndex) {
		const eventMsg = "signing not permitted due to Doppelganger protection"
		span.AddEvent(eventMsg)
		logger.Warn(eventMsg, fields.ValidatorIndex(validatorDuty.ValidatorIndex))

		return true, nil, nil
	}

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
		return false, partialMsg, fmt.Errorf("failed signing attestation data: %w", err)
	}

	attDataRoot, err := attestationData.HashTreeRoot()
	if err != nil {
		return false, partialMsg, fmt.Errorf("failed to hash attestation data: %w", err)
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

	return false, partialMsg, nil
}

// TODO finish edge case where some roots may be missing
func (r *CommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("base post consensus message processing")
	hasQuorum, quorumRoots, err := r.BaseRunner.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) {
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}

	vIndices := make([]uint64, 0, len(signedMsg.Messages))
	for _, msg := range signedMsg.Messages {
		vIndices = append(vIndices, uint64(msg.ValidatorIndex))
	}

	const eventMsg = "🧩 got partial signatures (post consensus)"
	span.AddEvent(eventMsg)
	logger.Debug(eventMsg,
		zap.Uint64("signer", ssvtypes.PartialSigMsgSigner(signedMsg)),
		zap.Uint64s("validators", vIndices),
		zap.Bool("quorum", hasQuorum),
		zap.Int("quorum_roots", len(quorumRoots)),
	)

	if !hasQuorum {
		return nil
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleCommittee)

	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := r.expectedPostConsensusRootsAndBeaconObjects(ctx, logger)
	if err != nil {
		return fmt.Errorf("could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		return ErrNoValidDutiesToExecute
	}

	attestationsToSubmit := make(map[phase0.ValidatorIndex]*spec.VersionedAttestation)
	syncCommitteeMessagesToSubmit := make(map[phase0.ValidatorIndex]*altair.SyncCommitteeMessage)

	// Get unique roots to avoid repetition
	deduplicatedRoots := make(map[[32]byte]struct{})
	for _, root := range quorumRoots {
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
			fields.BeaconRole(role),
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
				vLogger := logger.With(zap.Uint64("validator_index", uint64(validatorIndex)), zap.String("pubkey", hex.EncodeToString(pubKey[:])))

				sig, err := r.BaseRunner.State.ReconstructBeaconSig(r.BaseRunner.State.PostConsensusContainer, root, pubKey[:], validatorIndex)
				// If the reconstructed signature verification failed, fall back to verifying each partial signature
				if err != nil {
					for root := range roots {
						r.BaseRunner.FallBackAndVerifyEachSignature(r.BaseRunner.State.PostConsensusContainer, root, share.Committee, validatorIndex)
					}
					const eventMsg = "got post-consensus quorum but it has invalid signatures"
					span.AddEvent(eventMsg)
					vLogger.Error(eventMsg, zap.Error(err))

					errCh <- fmt.Errorf("%s: %w", eventMsg, err)
					return
				}

				vLogger.Debug("🧩 reconstructed partial signature")

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
					// Only mark as safe if this is an attester role
					// We want to mark the validator as safe as soon as possible to minimize unnecessary delays in enabling signing.
					// The doppelganger check is not performed for sync committee duties, so we rely on attester duties for safety confirmation.
					r.doppelgangerHandler.ReportQuorum(signatureResult.validatorIndex)

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

	attestations := make([]*spec.VersionedAttestation, 0, len(attestationsToSubmit))
	for _, att := range attestationsToSubmit {
		if att != nil && att.ValidatorIndex != nil {
			attestations = append(attestations, att)
		}
	}
	if len(attestations) > 0 {
		validators := make([]phase0.ValidatorIndex, 0, len(attestations))
		for _, att := range attestations {
			validators = append(validators, *att.ValidatorIndex)
		}
		aLogger := logger.With(zap.Any("validators", validators))

		const submittingAttestationsEvent = "submitting attestations"
		aLogger.Debug(submittingAttestationsEvent)
		span.AddEvent(submittingAttestationsEvent)

		submissionStart := time.Now()

		// Submit multiple attestations
		if err := r.beacon.SubmitAttestations(ctx, attestations); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleAttester)
			const errMsg = "could not submit attestations"
			aLogger.Error(errMsg, zap.Error(err))
			return fmt.Errorf("%s: %w", errMsg, err)
		}

		recordSuccessfulSubmission(ctx, int64(len(attestations)), r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.BaseRunner.State.CurrentDuty.DutySlot()), spectypes.BNRoleAttester)
		attData, err := attestations[0].Data()
		if err != nil {
			return fmt.Errorf("could not get attestation data: %w", err)
		}
		const eventMsg = "✅ successfully submitted attestations"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconBlockRootAttribute(attData.BeaconBlockRoot),
			observability.DutyRoundAttribute(r.BaseRunner.State.RunningInstance.State.Round),
			observability.ValidatorCountAttribute(len(attestations)),
		))
		aLogger.Info(eventMsg,
			fields.BlockRoot(attData.BeaconBlockRoot),
			fields.Took(time.Since(submissionStart)),
			fields.Count(len(attestations)),
		)

		// Record successful submissions
		for validator := range attestationsToSubmit {
			r.RecordSubmission(spectypes.BNRoleAttester, validator)
		}
	}

	// Submit multiple sync committee.
	syncCommitteeMessages := make([]*altair.SyncCommitteeMessage, 0, len(syncCommitteeMessagesToSubmit))
	for _, syncMsg := range syncCommitteeMessagesToSubmit {
		syncCommitteeMessages = append(syncCommitteeMessages, syncMsg)
	}
	if len(syncCommitteeMessages) > 0 {
		validators := make([]phase0.ValidatorIndex, 0, len(syncCommitteeMessages))
		for _, sc := range syncCommitteeMessages {
			validators = append(validators, sc.ValidatorIndex)
		}
		scLogger := logger.With(zap.Any("validators", validators))

		const submittingSyncCommitteeEvent = "submitting sync committee"
		scLogger.Debug(submittingSyncCommitteeEvent)
		span.AddEvent(submittingSyncCommitteeEvent)

		submissionStart := time.Now()
		if err := r.beacon.SubmitSyncMessages(ctx, syncCommitteeMessages); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleSyncCommittee)
			const errMsg = "could not submit sync committee messages"
			scLogger.Error(errMsg, zap.Error(err))
			return fmt.Errorf("%s: %w", errMsg, err)
		}

		syncMsgsCount := len(syncCommitteeMessages)
		if syncMsgsCount <= math.MaxUint32 {
			recordSuccessfulSubmission(
				ctx,
				int64(syncMsgsCount),
				r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.BaseRunner.State.CurrentDuty.DutySlot()),
				spectypes.BNRoleSyncCommittee,
			)
		}

		const eventMsg = "✅ successfully submitted sync committee"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconSlotAttribute(r.BaseRunner.State.CurrentDuty.DutySlot()),
			observability.DutyRoundAttribute(r.BaseRunner.State.RunningInstance.State.Round),
			observability.BeaconBlockRootAttribute(syncCommitteeMessages[0].BeaconBlockRoot),
			observability.ValidatorCountAttribute(len(syncCommitteeMessages)),
			attribute.Float64("ssv.validator.duty.submission_time", time.Since(submissionStart).Seconds()),
			attribute.Float64("ssv.validator.duty.consensus_time_total", time.Since(r.measurements.consensusStart).Seconds()),
		))
		scLogger.Info(eventMsg,
			fields.BlockRoot(syncCommitteeMessages[0].BeaconBlockRoot),
			fields.Took(time.Since(submissionStart)),
			fields.Count(syncMsgsCount),
		)

		// Record successful submissions
		for validator := range syncCommitteeMessagesToSubmit {
			r.RecordSubmission(spectypes.BNRoleSyncCommittee, validator)
		}
	}

	if executionErr != nil {
		return executionErr
	}

	if r.HasSubmittedAllValidatorDuties(attestationMap, committeeMap) {
		r.BaseRunner.State.Finished = true
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.RoleCommittee, r.BaseRunner.State.RunningInstance.State.Round)
		const dutyFinishedEvent = "✔️finished duty processing (100% success)"
		logger.Info(dutyFinishedEvent,
			fields.ConsensusTime(r.measurements.ConsensusTime()),
			fields.ConsensusRounds(uint64(r.state().RunningInstance.State.Round)),
			fields.PostConsensusTime(r.measurements.PostConsensusTime()),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
			fields.TotalDutyTime(r.measurements.TotalDutyTime()),
		)
		span.AddEvent(dutyFinishedEvent)
		span.SetStatus(codes.Ok, "")
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

func (r *CommitteeRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, timeoutData *ssvtypes.TimeoutData) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, timeoutData)
}

// HasSubmittedAllValidatorDuties -- Returns true if the runner has done submissions for all validators for the given slot
func (r *CommitteeRunner) HasSubmittedAllValidatorDuties(attestationMap map[phase0.ValidatorIndex][32]byte, syncCommitteeMap map[phase0.ValidatorIndex][32]byte) bool {
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
func (r *CommitteeRunner) RecordSubmission(role spectypes.BeaconRole, valIdx phase0.ValidatorIndex) {
	if _, ok := r.submittedDuties[role]; !ok {
		r.submittedDuties[role] = make(map[phase0.ValidatorIndex]struct{})
	}
	r.submittedDuties[role][valIdx] = struct{}{}
}

// HasSubmitted -- Returns true if there is a record of submission for the (role, validator index, slot) tuple
func (r *CommitteeRunner) HasSubmitted(role spectypes.BeaconRole, valIdx phase0.ValidatorIndex) bool {
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

	// look for the expectedRoot in the attestationMap
	for validator, root := range attestationMap {
		if root == expectedRoot {
			validators = append(validators, validator)
		}
	}
	if len(validators) > 0 {
		return spectypes.BNRoleAttester, validators, true
	}
	// look for the expectedRoot in the committeeMap
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

// expectedPreConsensusRootsAndDomain is not needed because there is no pre-consensus phase.
func (r *CommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no pre consensus roots for committee runner")
}

// expectedPostConsensusRootsAndDomain signature returns only one domain type... but we can have mixed domains
// instead we rely on expectedPostConsensusRootsAndBeaconObjects that is called later
func (r *CommitteeRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("unexpected expectedPostConsensusRootsAndDomain func call")
}

func (r *CommitteeRunner) expectedPostConsensusRootsAndBeaconObjects(ctx context.Context, logger *zap.Logger) (
	attestationMap map[phase0.ValidatorIndex][32]byte,
	syncCommitteeMap map[phase0.ValidatorIndex][32]byte,
	beaconObjects map[phase0.ValidatorIndex]map[[32]byte]any, err error,
) {
	attestationMap = make(map[phase0.ValidatorIndex][32]byte)
	syncCommitteeMap = make(map[phase0.ValidatorIndex][32]byte)
	beaconObjects = make(map[phase0.ValidatorIndex]map[[32]byte]any)
	duty := r.BaseRunner.State.CurrentDuty
	beaconVoteData := r.BaseRunner.State.DecidedValue
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(beaconVoteData); err != nil {
		return nil, nil, nil, fmt.Errorf("could not decode beacon vote: %w", err)
	}

	slot := duty.DutySlot()
	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(slot)
	dataVersion, _ := r.BaseRunner.NetworkConfig.ForkAtEpoch(epoch)

	for _, validatorDuty := range duty.(*spectypes.CommitteeDuty).ValidatorDuties {
		if validatorDuty == nil {
			continue
		}
		if err := r.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
			logger.Warn("duty is no longer valid", fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
			continue
		}
		logger := logger.With(fields.Validator(validatorDuty.PubKey[:]))
		slot := validatorDuty.DutySlot()
		epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(slot)
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
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]any)
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
				beaconObjects[validatorDuty.ValidatorIndex] = make(map[[32]byte]any)
			}
			beaconObjects[validatorDuty.ValidatorIndex][root] = syncMsg
		default:
			return nil, nil, nil, fmt.Errorf("invalid duty type: %s", validatorDuty.Type)
		}
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

func (r *CommitteeRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "execute_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	r.measurements.StartDutyFlow()

	start := time.Now()
	slot := duty.DutySlot()

	attData, _, err := r.GetBeaconNode().GetAttestationData(ctx, slot)
	if err != nil {
		return fmt.Errorf("failed to get attestation data: %w", err)
	}

	const attestationDataFetchedEvent = "fetched attestation data from CL"
	logger.Debug(attestationDataFetchedEvent, fields.Took(time.Since(start)))
	span.AddEvent(attestationDataFetchedEvent)

	vote := &spectypes.BeaconVote{
		BlockRoot: attData.BeaconBlockRoot,
		Source:    attData.Source,
		Target:    attData.Target,
	}

	r.measurements.StartConsensus()
	r.ValCheck = ssv.NewVoteChecker(
		r.signer,
		slot,
		r.attestingValidators,
		r.GetNetworkConfig().EstimatedCurrentEpoch(),
		vote,
	)
	if err := r.BaseRunner.decide(ctx, logger, duty.DutySlot(), vote, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}

	return nil
}

func (r *CommitteeRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *CommitteeRunner) GetDoppelgangerHandler() DoppelgangerProvider {
	return r.doppelgangerHandler
}

func (r *CommitteeRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func constructAttestationData(vote *spectypes.BeaconVote, duty *spectypes.ValidatorDuty, version spec.DataVersion) *phase0.AttestationData {
	attData := &phase0.AttestationData{
		Slot:            duty.Slot,
		Index:           duty.CommitteeIndex,
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
	if version >= spec.DataVersionElectra {
		attData.Index = 0 // EIP-7549: Index should be set to 0
	}
	return attData
}
