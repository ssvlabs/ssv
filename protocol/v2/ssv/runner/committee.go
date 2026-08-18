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

	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
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

type CommitteeDutyGuard interface {
	StartDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error
	ValidDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error
}

type CommitteeRunner struct {
	*BaseRunner

	// attestingValidators is a list of validator this committee-runner will be processing attestation duties for.
	attestingValidators []phase0.BLSPubKey

	network             protocolp2p.Network
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

// CommitteeRunnerOptions bundles all dependencies required by NewCommitteeRunner.
type CommitteeRunnerOptions struct {
	BaseRunnerOptions

	AttestingValidators []phase0.BLSPubKey
	QBFTController      *controller.Controller
	DutyGuard           CommitteeDutyGuard
	DoppelgangerHandler DoppelgangerProvider
}

func NewCommitteeRunner(opts CommitteeRunnerOptions) (Runner, error) {
	if len(opts.Share) == 0 {
		return nil, errors.New("no shares")
	}

	return &CommitteeRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleCommittee,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
			QBFTController: opts.QBFTController,
		},

		attestingValidators: opts.AttestingValidators,

		beacon:              opts.Beacon,
		network:             opts.Network,
		signer:              opts.Signer,
		operatorSigner:      opts.OperatorSigner,
		submittedDuties:     make(map[spectypes.BeaconRole]map[phase0.ValidatorIndex]struct{}),
		DutyGuard:           opts.DutyGuard,
		doppelgangerHandler: opts.DoppelgangerHandler,
		measurements:        newMeasurementsStore(),
	}, nil
}

func (r *CommitteeRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	committeeDuty, err := committeeDutyFromDuty(duty)
	if err != nil {
		return fmt.Errorf("committee duty: %w", err)
	}

	span.SetAttributes(observability.DutyCountAttribute(len(committeeDuty.ValidatorDuties)))

	for _, validatorDuty := range committeeDuty.ValidatorDuties {
		err := r.DutyGuard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), committeeDuty.DutySlot())
		if err != nil {
			return fmt.Errorf(
				"could not start %s duty at slot %d for validator %x: %w",
				validatorDuty.Type, committeeDuty.DutySlot(), validatorDuty.PubKey, err,
			)
		}
	}
	err = r.baseStartNewDuty(ctx, logger, r, committeeDuty, quorum)
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
	return json.Unmarshal(data, r)
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
		network        protocolp2p.Network
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
		network        protocolp2p.Network
		signer         ekm.BeaconSigner
		operatorSigner ssvtypes.OperatorSigner
		valCheck       ssv.ValueChecker
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeRunnerAlias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
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

func (r *CommitteeRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *CommitteeRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *CommitteeRunner) GetBeaconSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *CommitteeRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre consensus phase for committee runner")
}

func (r *CommitteeRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("processing QBFT consensus msg")
	decided, decidedValue, err := r.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, msg, &spectypes.BeaconVote{})
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleCommittee)

	committeeDuty, err := r.currentCommitteeDuty()
	if err != nil {
		return fmt.Errorf("current committee duty: %w", err)
	}
	committeeDutySlot := committeeDuty.DutySlot()
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     committeeDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	epoch := r.NetworkConfig.EstimatedEpochAtSlot(committeeDutySlot)
	version, _ := r.NetworkConfig.ForkAtEpoch(epoch)

	span.SetAttributes(
		observability.BeaconSlotAttribute(committeeDutySlot),
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

		totalAttesterDuties,
		totalSyncCommitteeDuties,
		blockedAttesterDuties atomic.Uint32
	)

	beaconVote, err := beaconVoteFromEncoder(decidedValue)
	if err != nil {
		return fmt.Errorf("beacon vote: %w", err)
	}

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
						r.NetworkConfig,
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
		// Benign terminal: the committee decided but this operator ended up with zero valid duties to
		// sign. Conclude as not_required so the watcher doesn't report a false "stuck"; the sentinel
		// still tells committee_queue to drop the message and terminate the runner.
		r.markDutyNotRequired()
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
			r.NetworkConfig.DomainTypeAtSlot(r.State.CurrentDuty.DutySlot()),
			r.QBFTController.CommitteeMember.CommitteeID[:],
			r.RunnerRoleType,
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
		OperatorIDs: []spectypes.OperatorID{r.QBFTController.CommitteeMember.OperatorID},
		SSVMessage:  ssvMsg,
	}

	r.measurements.StartPostConsensus()
	if err := r.GetNetwork().BroadcastAtSlot(msgToBroadcast, postConsensusMsg.Slot); err != nil {
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
		r.NetworkConfig,
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

func (r *CommitteeRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("base post consensus message processing")
	hasQuorum, roots, err := r.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) {
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}

	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to submitting. Pre-quorum waiting, full success
	// (markDutySucceeded) and partial progress all return nil, so this only fires on a terminal
	// post-quorum error — report it as failed instead of letting it fall through to a false "stuck".
	// Unlike the consensus phase, a failure here is final: submission is the duty's last step.
	// The one exception is a recoverable BLS-reconstruction failure: the offending partial sig has
	// already been dropped by the fallback, so a later message can re-cross quorum and retry — those
	// are tagged recoverableReconstructError and must not be recorded as failed.
	// Shutdown (context cancellation) needs no special-casing — markDutyFailed drops a context.Canceled
	// reason, so a submission aborted by shutdown isn't recorded as a failure.
	// The benign no-beacon-objects sentinel (ErrNoValidDutiesToExecute) pre-concludes the duty as
	// not_required before returning, which makes this deferred markDutyFailed a no-op (concludeDuty
	// is idempotent) — it must not be recorded as failed either.
	defer func() {
		if err != nil && !isRecoverableReconstructError(err) {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleCommittee)

	// Get validator-root maps for attestations and sync committees, and the root-beacon object map
	attestationMap, committeeMap, beaconObjects, err := r.expectedPostConsensusRootsAndBeaconObjects(ctx, logger)
	if err != nil {
		return fmt.Errorf("could not get expected post consensus roots and beacon objects: %w", err)
	}
	if len(beaconObjects) == 0 {
		// Benign terminal: the committee reached consensus but this operator has no beacon objects to
		// submit (divergent validator sets across the committee's operators — every duty was skipped
		// as guard-invalid). An empty map here is guaranteed benign: an all-construction-failure empty
		// result is surfaced as an error by expectedPostConsensusRootsAndBeaconObjects above and
		// classified failed by the defer. Conclude as not_required before returning the sentinel;
		// concludeDuty is idempotent, so the deferred markDutyFailed becomes a no-op. The sentinel
		// still tells committee_queue to drop the message and terminate the runner.
		r.markDutyNotRequired()
		return ErrNoValidDutiesToExecute
	}

	attestationsToSubmit := make(map[phase0.ValidatorIndex]*spec.VersionedAttestation)
	syncCommitteeMessagesToSubmit := make(map[phase0.ValidatorIndex]*altair.SyncCommitteeMessage)

	// Recoverable reconstruct failures are discriminated by the recoverableReconstructError tag rather
	// than by a spec error code — the tag also covers the uncoded BLS Deserialize/Recover failures.
	// The sibling AggregatorCommitteeRunner instead classifies by the PostConsensusQuorumWithInvalidSignatures
	// code; the divergence is deliberate (tagging with that code there breaks its spectest fixtures).
	var recoverableErr, terminalErr error
	// classify is the single source of truth for the terminal/recoverable split, shared by the listener
	// receive site and the post-listener drain so the two can never drift apart. Recoverable failures
	// carry the recoverableReconstructError tag; anything arriving without it is treated as terminal.
	classify := func(err error) {
		if isRecoverableReconstructError(err) {
			recoverableErr = err
		} else {
			terminalErr = err
		}
	}

	span.SetAttributes(observability.BeaconBlockRootCountAttribute(len(roots)))
	// For each root that got at least one quorum, find the duties associated to it and try to submit
	for i, root := range roots {
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
			// As per the comments below, the quorums (for root+validator pairs) we got from basePostConsensusMsgProcessing
			// call above are optimistic - some of these quorums might have been invalidated now, hence, to avoid an
			// unnecessary unsuccessful BLS signature reconstruction attempt we need to check if root+validator pair
			// still has quorum.
			gotQuorum, quorumSigners := r.State.PostConsensusContainer.HasQuorum(validator, root)
			if !gotQuorum {
				continue
			}
			// Skip if already submitted
			if r.HasSubmitted(role, validator) {
				continue
			}

			wg.Add(1)
			go func(validatorIndex phase0.ValidatorIndex, root [32]byte) {
				defer wg.Done()

				share := r.Share[validatorIndex]
				// Operators might have diverging views on which validators they have in a committee
				// (e.g., an operator might have not yet seen an ValidatorAdded event,
				// or failed to process it and moved on). Hence, we need to check for this explicitly every time.
				if share == nil {
					return
				}
				pubKey := share.ValidatorPubKey

				vLogger := logger.With(
					zap.Uint64("validator_index", uint64(validatorIndex)),
					zap.String("pubkey", hex.EncodeToString(pubKey[:])),
					fields.BlockRoot(root),
					zap.Uint64s("quorum_signers", quorumSigners),
				)

				sig, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, pubKey[:], validatorIndex)
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
						r.FallBackAndVerifyEachSignature(r.State.PostConsensusContainer, root, share.Committee, validatorIndex)
					}
					const eventMsg = "got post-consensus quorum but it has invalid signatures"
					span.AddEvent(eventMsg)
					vLogger.Error(eventMsg, zap.Error(err))

					// FallBackAndVerifyEachSignature ran above for any reconstruct error, so this is
					// recoverable by construction: tag it at the push site rather than inferring
					// recoverability from a spec error code at the receive site (the code is only
					// attached by VerifyReconstructedSignature — the earlier Deserialize/Recover step
					// returns an uncoded but equally recoverable error).
					errCh <- recoverableReconstructError{fmt.Errorf("%s: %w", eventMsg, err)}
					return
				}

				vLogger.Debug("🧩 reconstructed partial signature")

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
				// markDutyFailed (via the defer) drops context.Canceled internally, so shutdown
				// isn't recorded as a failure.
				return ctx.Err()
			case err := <-errCh:
				classify(err)
			case signatureResult, ok := <-signatureCh:
				if !ok {
					break listener
				}

				validatorObjects, exists := beaconObjects[signatureResult.validatorIndex]
				if !exists {
					terminalErr = fmt.Errorf("could not find beacon object for validator index: %d", signatureResult.validatorIndex)
					continue
				}
				sszObject, exists := validatorObjects[root]
				if !exists {
					terminalErr = fmt.Errorf("could not find ssz object for root: %s", root)
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
						terminalErr = fmt.Errorf("could not insert signature in versioned attestation")
						continue
					}

					attestationsToSubmit[signatureResult.validatorIndex] = att
				}
			}
		}

		// Drain any error still buffered on errCh: when signatureCh closes in the same iteration the select
		// may take the close branch and skip it. All workers have finished (signatureCh closes only after
		// wg.Wait), so this non-blocking drain is complete. Today errCh carries only the recoverable
		// reconstruct error (the sole producer above), and dropping one is benign (the duty stays open for
		// retry). The drain is defensive: it classifies that error for completeness and future-proofs the
		// path should a terminal error ever be pushed here.
	drainErrCh:
		for {
			select {
			case err := <-errCh:
				classify(err)
			default:
				break drainErrCh
			}
		}

		logger.Debug("🧩 reconstructed partial signatures for root", fields.BlockRoot(root))
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

		currentDutySlot, err := r.currentDutySlot()
		if err != nil {
			return fmt.Errorf("current duty slot: %w", err)
		}
		recordSuccessfulSubmission(ctx, int64(len(attestations)), r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot), spectypes.BNRoleAttester)
		attData, err := attestations[0].Data()
		if err != nil {
			return fmt.Errorf("could not get attestation data: %w", err)
		}
		const eventMsg = "✅ successfully submitted attestations"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconBlockRootAttribute(attData.BeaconBlockRoot),
			observability.DutyRoundAttribute(r.State.RunningInstance.State.Round),
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
			currentDutySlot, err := r.currentDutySlot()
			if err != nil {
				return fmt.Errorf("current duty slot: %w", err)
			}
			recordSuccessfulSubmission(
				ctx,
				int64(syncMsgsCount),
				r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot),
				spectypes.BNRoleSyncCommittee,
			)
		}

		currentDutySlot, err := r.currentDutySlot()
		if err != nil {
			return fmt.Errorf("current duty slot: %w", err)
		}
		const eventMsg = "✅ successfully submitted sync committee"
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.BeaconSlotAttribute(currentDutySlot),
			observability.DutyRoundAttribute(r.State.RunningInstance.State.Round),
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

	// A terminal error on any root wins over a recoverable one. executionErr used to be a single
	// last-write-wins variable, so when a genuine failure and a recoverable reconstruct error happened
	// in the same round, classification depended on goroutine/root ordering. Splitting the two keeps it
	// deterministic — a real failure is always recorded as failed, never mislabeled as recoverable/stuck.
	// The defer classifies both: terminalErr → markDutyFailed; recoverableErr carries its tag → excluded.
	if terminalErr != nil {
		return terminalErr
	}
	if recoverableErr != nil {
		// Reconstruct-invalid-sigs is recoverable: FallBackAndVerifyEachSignature can drop the root
		// below quorum, so a later partial-sig message re-crosses quorum and re-enters this loop to
		// retry pending roots (already-submitted roots are skipped via HasSubmitted). Returned with its
		// recoverableReconstructError tag so the defer does not record the duty as failed.
		return recoverableErr
	}

	if r.HasSubmittedAllValidatorDuties(attestationMap, committeeMap) {
		r.markDutySucceeded()
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.RoleCommittee, r.State.RunningInstance.State.Round)
		const dutyFinishedEvent = "✔️finished duty processing (100% success)"
		logger.Info(dutyFinishedEvent,
			fields.ConsensusTime(r.measurements.ConsensusTime()),
			fields.ConsensusRounds(uint64(r.State.RunningInstance.State.Round)),
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
		fields.ConsensusRounds(uint64(r.State.RunningInstance.State.Round)),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)
	span.AddEvent(dutyFinishedEvent)

	return nil
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
	committeeDuty, err := r.currentCommitteeDuty()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("current committee duty: %w", err)
	}
	beaconVoteData := r.State.DecidedValue
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(beaconVoteData); err != nil {
		return nil, nil, nil, fmt.Errorf("could not decode beacon vote: %w", err)
	}

	slot := committeeDuty.DutySlot()
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(slot)
	dataVersion, _ := r.NetworkConfig.ForkAtEpoch(epoch)

	// Skips fall into two classes: guard invalidations are benign (the #2903 divergent-validator-sets
	// case — the duty is genuinely not this operator's to submit), while construction / domain-data /
	// signing-root failures mean a submission was missed. The distinction only matters when NOTHING
	// could be built: partial failures keep the per-validator debug-and-continue behavior so one
	// validator's failure never blocks the others' submissions, but an all-failure empty result must
	// surface as an error — otherwise the caller's len(beaconObjects)==0 branch would conclude the
	// duty not_required, masking the miss (the sibling AggregatorCommitteeRunner surfaces these
	// errors for the same reason).
	var constructionErr error

	for _, validatorDuty := range committeeDuty.ValidatorDuties {
		if validatorDuty == nil {
			continue
		}
		if err := r.DutyGuard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.DutySlot()); err != nil {
			logger.Warn("duty is no longer valid", fields.Validator(validatorDuty.PubKey[:]), fields.BeaconRole(validatorDuty.Type), zap.Error(err))
			continue
		}
		logger := logger.With(fields.Validator(validatorDuty.PubKey[:]))
		slot := validatorDuty.DutySlot()
		epoch := r.NetworkConfig.EstimatedEpochAtSlot(slot)
		switch validatorDuty.Type {
		case spectypes.BNRoleAttester:
			// Attestation object
			attestationData := constructAttestationData(beaconVote, validatorDuty, dataVersion)
			attestationResponse, err := specssv.ConstructVersionedAttestationWithoutSignature(attestationData, dataVersion, validatorDuty)
			if err != nil {
				logger.Debug("failed to construct attestation", zap.Error(err))
				constructionErr = errors.Join(constructionErr, fmt.Errorf("construct attestation (validator %d): %w", validatorDuty.ValidatorIndex, err))
				continue
			}

			// Root
			domain, err := r.GetBeaconNode().DomainData(ctx, epoch, spectypes.DomainAttester)
			if err != nil {
				logger.Debug("failed to get attester domain", zap.Error(err))
				constructionErr = errors.Join(constructionErr, fmt.Errorf("get attester domain (validator %d): %w", validatorDuty.ValidatorIndex, err))
				continue
			}

			root, err := spectypes.ComputeETHSigningRoot(attestationData, domain)
			if err != nil {
				logger.Debug("failed to compute attester root", zap.Error(err))
				constructionErr = errors.Join(constructionErr, fmt.Errorf("compute attester root (validator %d): %w", validatorDuty.ValidatorIndex, err))
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
				constructionErr = errors.Join(constructionErr, fmt.Errorf("get sync committee domain (validator %d): %w", validatorDuty.ValidatorIndex, err))
				continue
			}
			// Eth root
			blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
			root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
			if err != nil {
				logger.Debug("failed to compute sync committee root", zap.Error(err))
				constructionErr = errors.Join(constructionErr, fmt.Errorf("compute sync committee root (validator %d): %w", validatorDuty.ValidatorIndex, err))
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
	if len(beaconObjects) == 0 && constructionErr != nil {
		return nil, nil, nil, fmt.Errorf("no beacon objects could be built: %w", constructionErr)
	}
	return attestationMap, syncCommitteeMap, beaconObjects, nil
}

func (r *CommitteeRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	span := trace.SpanFromContext(ctx)

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
		vote,
	)
	if err := r.decide(ctx, logger, duty.DutySlot(), vote, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}

	return nil
}

func (r *CommitteeRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *CommitteeRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *CommitteeRunner) GetDoppelgangerHandler() DoppelgangerProvider {
	return r.doppelgangerHandler
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
