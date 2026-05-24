package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/twoab"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// consensusVariant selects which OBFT-family protocol a ProposerRunner drives.
// Exactly one variant is active per runner, chosen at construction from which
// controller the caller supplies.
type consensusVariant uint8

const (
	// variantOBFT drives bare OBFT (protocol/v2/obft/base via runner/obft).
	variantOBFT consensusVariant = iota
	// variantTwoab drives 2abOBFT (protocol/v2/obft/twoab via runner/obft/twoab).
	variantTwoab
)

// ProposerRunner runs the proposer duty using an OBFT-family protocol for
// consensus (bare OBFT or 2abOBFT, selected per cluster; see docs/OBFT.md).
// Construction requires exactly one of OBFTController or TwoabController.
//
// The two variants share all beacon-side lifecycle hooks (FetchCandidate /
// HostValidate / SubmitOutput / OnMissedSlot) — those operate on the shared
// obft.Output type and the variant-identical candidate encoding — and the
// per-slot scratch state (obftSlots). Only the consensus driver wiring (which
// Scheduler / RateLimiter / dispatch + the broadcast envelope's MsgType)
// differs; the 2ab-specific glue lives in proposer_twoab.go, the OBFT glue in
// proposer_obft.go.
type ProposerRunner struct {
	*BaseRunner

	beacon              beacon.BeaconNode
	network             specqbft.Network
	signer              ekm.BeaconSigner
	operatorSigner      ssvtypes.OperatorSigner
	doppelgangerHandler DoppelgangerProvider
	measurements        *dutyMeasurements
	graffiti            []byte

	// proposerDelay allows Operator to configure a delay to wait out before requesting Ethereum
	// block to propose if this Operator is proposer-duty Leader. This allows Operator to extract
	// higher MEV.
	proposerDelay time.Duration

	// variant selects the active consensus driver (OBFT vs 2abOBFT).
	variant consensusVariant

	// OBFT machinery. Non-nil iff variant == variantOBFT. Owned by the runner;
	// constructed in NewProposerRunner from the caller-supplied Controller plus
	// runner-bound LifecycleHooks.
	obftCtrl  *obftadapter.Controller
	obftSched *obftadapter.Scheduler
	obftRL    *obftadapter.RateLimiter

	// 2abOBFT machinery. Non-nil iff variant == variantTwoab. Mirrors the OBFT
	// trio above; the beacon-side hooks and obftSlots scratch are shared.
	twoabCtrl  *twoabadapter.Controller
	twoabSched *twoabadapter.Scheduler
	twoabRL    *twoabadapter.RateLimiter

	// obftSlots carries per-slot scratch state (RANDAO sig, fetched block
	// version) that the lifecycle hooks need but the protocol wire types don't
	// carry. Shared by both variants (the scratch is variant-neutral).
	obftMu    sync.Mutex
	obftSlots map[phase0.Slot]*obftSlotState
}

// obftSlotState holds the per-slot scratch space the OBFT lifecycle
// hooks need: the RANDAO signature for block fetches, the spec version
// observed at fetch time (so SubmitOutput can decode the agreed-upon
// blinded block), the cached full block (for full-block submission when
// the decided V matches what this operator fetched), and the cancel func
// for the slot's driver goroutine.
//
// All fields are written by `obftFetchCandidate` and read by
// `obftSubmitOutput`; both run on different goroutines, so all access
// goes through `ProposerRunner.obftMu`. Per-slot scoping prevents the
// previous slot's cached block from leaking into the next slot's submit.
type obftSlotState struct {
	randao           phase0.BLSSignature
	cancel           context.CancelFunc
	cachedFullBlock  *api.VersionedProposal
	cachedBlindedSSZ []byte
	version          spec.DataVersion // set when this operator fetches a candidate; zero otherwise
	slotStart        time.Time        // wall-clock slot-start, used to compute observedOffset for Phase-1 bundles
}

// ProposerRunnerOptions bundles all dependencies required by NewProposerRunner.
type ProposerRunnerOptions struct {
	BaseRunnerOptions

	DoppelgangerHandler DoppelgangerProvider
	HighestDecidedSlot  phase0.Slot
	Graffiti            []byte
	// ProposerDelay allows Operator to configure a delay to wait out before requesting Ethereum
	// block to propose if this Operator is proposer-duty Leader. This allows Operator to extract
	// higher MEV.
	ProposerDelay time.Duration

	// OBFTController owns the cluster's bare-OBFT primitives (BLSSigner for
	// value-signing, KyberSigner for IBE-tag signing under the DST-trick
	// approach, TLockIBE for layer encryption, plus the pubkey-shares map and
	// committee). See protocol/v2/ssv/runner/obft.
	//
	// Exactly one of OBFTController / TwoabController must be set: the non-nil
	// one selects the runner's consensus variant. NewProposerRunner errors if
	// neither or both are set.
	OBFTController *obftadapter.Controller

	// TwoabController owns the cluster's 2abOBFT primitives — the same crypto
	// shape as OBFTController, driving the split-Phase-2 (Value/NoValue + dynamic
	// commit) protocol. See protocol/v2/ssv/runner/obft/twoab.
	TwoabController *twoabadapter.Controller
}

func NewProposerRunner(opts ProposerRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}
	if (opts.OBFTController == nil) == (opts.TwoabController == nil) {
		return nil, errors.New("exactly one of OBFTController / TwoabController is required for ProposerRunner")
	}

	r := &ProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleProposer,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
			// QBFTController stays nil for the proposer — the QBFT
			// consensus path was removed in favor of OBFT. BaseRunner
			// methods that read QBFTController are nil-safe (or have
			// been made so).
			highestDecidedSlot: opts.HighestDecidedSlot,
		},

		beacon:              opts.Beacon,
		network:             opts.Network,
		signer:              opts.Signer,
		operatorSigner:      opts.OperatorSigner,
		doppelgangerHandler: opts.DoppelgangerHandler,
		measurements:        newMeasurementsStore(),
		graffiti:            opts.Graffiti,

		proposerDelay: opts.ProposerDelay,
	}
	r.obftSlots = make(map[phase0.Slot]*obftSlotState)

	if opts.TwoabController != nil {
		// 2abOBFT variant. Beacon-side hooks are shared with OBFT (same
		// obft.Output type + identical candidate encoding); only Broadcast /
		// BroadcastCertificate (distinct MsgType) and OnReplayError (2ab wire
		// kind) are variant-specific. See proposer_twoab.go.
		hooks := &twoabadapter.LifecycleHooks{
			FetchCandidate:       r.obftFetchCandidate,
			HostValidate:         r.obftHostValidate,
			Broadcast:            r.twoabBroadcast,
			SubmitOutput:         r.obftSubmitOutput,
			BroadcastCertificate: r.twoabBroadcastCertificate,
			OnMissedSlot:         r.obftOnMissedSlot,
			OnReplayError:        r.twoabOnReplayError,
		}
		sched, err := twoabadapter.NewScheduler(opts.TwoabController, hooks)
		if err != nil {
			return nil, fmt.Errorf("build 2abOBFT scheduler: %w", err)
		}
		r.variant = variantTwoab
		r.twoabCtrl = opts.TwoabController
		r.twoabSched = sched
		r.twoabRL = twoabadapter.NewRateLimiter()
		return r, nil
	}

	hooks := &obftadapter.LifecycleHooks{
		FetchCandidate:       r.obftFetchCandidate,
		HostValidate:         r.obftHostValidate,
		Broadcast:            r.obftBroadcast,
		SubmitOutput:         r.obftSubmitOutput,
		BroadcastCertificate: r.obftBroadcastCertificate,
		OnMissedSlot:         r.obftOnMissedSlot,
		OnReplayError:        r.obftOnReplayError,
	}
	sched, err := obftadapter.NewScheduler(opts.OBFTController, hooks)
	if err != nil {
		return nil, fmt.Errorf("build OBFT scheduler: %w", err)
	}
	r.variant = variantOBFT
	r.obftCtrl = opts.OBFTController
	r.obftSched = sched
	r.obftRL = obftadapter.NewRateLimiter()

	return r, nil
}

func (r *ProposerRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	return r.baseStartNewDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *ProposerRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutyFinished) {
		// Since we are re-using the same runner for different duties, ErrRunningDutyFinished error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing randao message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		return nil
	}

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleProposer)

	// only 1 root, verified in expectedPreConsensusRootsAndDomain
	root := roots[0]

	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// Sleep the remaining proposerDelay since slot start, ensuring on-time proposals even if duty began late.
	if timeLeft := r.remainingProposerDelay(duty.Slot, time.Now()); timeLeft > 0 {
		select {
		case <-time.After(timeLeft):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	waitedOutProposerDelayEvent := fmt.Sprintf("waited out proposer delay of %dms", r.proposerDelay.Milliseconds())
	logger.Debug(waitedOutProposerDelayEvent)
	span.AddEvent(waitedOutProposerDelayEvent)

	duty, err = r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// Hand off to the OBFT-family driver for the active variant. Each layer
	// leader fetches via the FetchCandidate hook at its own FetchAt offset;
	// SubmitOutput delivers the agreed-upon block to the beacon node. RANDAO is
	// plumbed via per-slot state so the FetchCandidate hook can use it.
	r.measurements.StartConsensus()
	if r.variant == variantTwoab {
		return r.twoabStartSlot(ctx, logger, duty.Slot, fullSig)
	}
	return r.obftStartSlot(ctx, logger, duty.Slot, fullSig)
}

// ProcessConsensus is unused on the proposer — OBFT carries its own
// envelope type (SSVOBFTMsgType, see proposer_obft.go::ProcessOBFTEnvelopeMsg).
// QBFT consensus messages arriving at a proposer runner indicate a
// misrouted message or a peer running an older binary; we surface them
// as an error so the network layer logs and drops them.
func (r *ProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return fmt.Errorf("proposer runner: QBFT consensus messages are not handled (OBFT-family only)")
}

// ProcessPostConsensus is unused on the proposer — OBFT folds post-
// consensus aggregation into Phase 3 of the protocol, surfaced via the
// SubmitOutput hook (proposer_obft.go::obftSubmitOutput). Post-consensus
// partial-signature messages arriving at a proposer runner are treated
// the same way as stray QBFT consensus messages above.
func (r *ProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return fmt.Errorf("proposer runner: QBFT post-consensus messages are not handled (OBFT-family only)")
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("current duty slot: %w", err)
	}
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot)
	return []ssz.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomain is part of the Runner interface
// but unused on the proposer — OBFT doesn't run a post-consensus partial-
// sig collection phase (the reconstructed block-root signature falls out
// of Phase 3's IBE walk, see proposer_obft.go::obftSubmitOutput).
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, phase0.DomainType{}, fmt.Errorf("proposer runner: no post-consensus phase (OBFT-family only)")
}

// executeDuty steps:
// 1) sign a partial randao sig and wait for 2f+1 partial sigs from peers
// 2) reconstruct randao and send GetBeaconBlock to BN
// 3) start consensus on duty + block data
// 4) Once consensus decides, sign partial block and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid block sig to the BN
func (r *ProposerRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	r.measurements.StartDutyFlow()

	proposerDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	if !r.doppelgangerHandler.CanSign(proposerDuty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(proposerDuty.ValidatorIndex))
		return nil
	}

	// sign partial randao
	span.AddEvent("signing beacon object")
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		proposerDuty,
		spectypes.SSZUint64(epoch),
		proposerDuty.DutySlot(),
		spectypes.DomainRandao,
	)
	if err != nil {
		return fmt.Errorf("could not sign randao: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     proposerDuty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	logger.Debug("signing and broadcasting randao partial sig", fields.Slot(duty.DutySlot()))

	r.measurements.StartPreConsensus()
	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast randao partial sig: %w", err)
	}

	return nil
}

func (r *ProposerRunner) remainingProposerDelay(slot phase0.Slot, now time.Time) time.Duration {
	slotTime := r.NetworkConfig.SlotStartTime(slot)
	proposeTime := slotTime.Add(r.proposerDelay)
	if wait := proposeTime.Sub(now); wait > 0 {
		return wait
	}
	return 0
}

func (r *ProposerRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *ProposerRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ProposerRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *ProposerRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *ProposerRunner) MarshalJSON() ([]byte, error) {
	type proposerRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}

	return json.Marshal(&proposerRunnerJSON{
		BaseRunner: r.BaseRunner,
	})
}

func (r *ProposerRunner) UnmarshalJSON(data []byte) error {
	type proposerRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}

	aux := &proposerRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}

	r.BaseRunner = aux.BaseRunner
	// Runtime dependencies (OBFT controller, hooks, signers, ekm signer,
	// beacon, network, doppelganger, measurements, …) are NOT restored
	// from JSON. Callers must rehydrate them explicitly via
	// NewProposerRunner before using a decoded runner.
	return nil
}

// Encode returns the encoded struct in bytes or error
func (r *ProposerRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ProposerRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

// GetRoot returns the root used for signing and verification
func (r *ProposerRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode ProposerRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

type executionInfo struct {
	BlockHash   phase0.Hash32
	ParentHash  phase0.Hash32
	BlockNumber uint64
}

// extractExecutionInfo extracts execution-layer info (hashes and block number) from a VersionedProposal.
// It handles both regular and blinded blocks across all supported versions.
func extractExecutionInfo(vBlk *api.VersionedProposal) (executionInfo, error) {
	if vBlk == nil {
		return executionInfo{}, fmt.Errorf("block is nil")
	}

	switch vBlk.Version {
	case spec.DataVersionCapella:
		if vBlk.Blinded {
			if vBlk.CapellaBlinded == nil || vBlk.CapellaBlinded.Body == nil ||
				vBlk.CapellaBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("capella blinded block data missing")
			}
			h := vBlk.CapellaBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Capella == nil || vBlk.Capella.Body == nil ||
			vBlk.Capella.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("capella block data missing")
		}
		p := vBlk.Capella.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	case spec.DataVersionDeneb:
		if vBlk.Blinded {
			if vBlk.DenebBlinded == nil || vBlk.DenebBlinded.Body == nil ||
				vBlk.DenebBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("deneb blinded block data missing")
			}
			h := vBlk.DenebBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Deneb == nil || vBlk.Deneb.Block == nil || vBlk.Deneb.Block.Body == nil ||
			vBlk.Deneb.Block.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("deneb block data missing")
		}
		p := vBlk.Deneb.Block.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	case spec.DataVersionElectra:
		if vBlk.Blinded {
			if vBlk.ElectraBlinded == nil || vBlk.ElectraBlinded.Body == nil ||
				vBlk.ElectraBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("electra blinded block data missing")
			}
			h := vBlk.ElectraBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Electra == nil || vBlk.Electra.Block == nil || vBlk.Electra.Block.Body == nil ||
			vBlk.Electra.Block.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("electra block data missing")
		}
		p := vBlk.Electra.Block.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	case spec.DataVersionFulu:
		if vBlk.Blinded {
			if vBlk.FuluBlinded == nil || vBlk.FuluBlinded.Body == nil ||
				vBlk.FuluBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("fulu blinded block data missing")
			}
			h := vBlk.FuluBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Fulu == nil || vBlk.Fulu.Block == nil || vBlk.Fulu.Block.Body == nil ||
			vBlk.Fulu.Block.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("fulu block data missing")
		}
		p := vBlk.Fulu.Block.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	default:
		return executionInfo{}, fmt.Errorf("unsupported block version %d", vBlk.Version)
	}
}
