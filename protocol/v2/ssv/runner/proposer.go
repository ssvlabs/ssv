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
	tbftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/tbft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// ProposerRunner runs the proposer duty using TBFT for consensus. The QBFT
// path was removed in favor of TBFT exclusively (see docs/TBFT.md +
// docs/IBE-INTEGRATION.md). Construction without a TBFTController is an
// error.
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

	// cachedFullBlock holds the initially fetched full (non-blinded) block
	// for this duty on this operator, if any. The TBFT SubmitOutput hook
	// uses it to submit the full block + blobs (Deneb/Electra/Fulu) when
	// the decided value matches what this operator originally fetched —
	// otherwise it falls back to the agreed-upon blinded block.
	cachedFullBlock *api.VersionedProposal
	// cachedBlindedBlockSSZ is the SSZ-blinded fingerprint of cachedFullBlock,
	// stored for byte-equality matching against the decided value at
	// SubmitOutput time.
	cachedBlindedBlockSSZ []byte

	// TBFT machinery. Owned by the runner; constructed in NewProposerRunner
	// from the caller-supplied Controller plus runner-bound LifecycleHooks.
	// `tbftSlots` carries per-slot scratch state (RANDAO sig, fetched block
	// version) that the lifecycle hooks need but the protocol wire types
	// don't carry.
	tbftCtrl  *tbftadapter.Controller
	tbftSched *tbftadapter.Scheduler
	tbftRL    *tbftadapter.RateLimiter

	tbftMu    sync.Mutex
	tbftSlots map[phase0.Slot]*tbftSlotState
}

// tbftSlotState holds the per-slot scratch space the TBFT lifecycle
// hooks need: the RANDAO signature for block fetches, the spec version
// observed at fetch time (so SubmitOutput can decode the agreed-upon
// blinded block), and the cancel func for the slot's driver goroutine.
type tbftSlotState struct {
	randao  phase0.BLSSignature
	cancel  context.CancelFunc
	version spec.DataVersion // set when this operator fetches a candidate; zero otherwise
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

	// TBFTController is required. It owns the cluster's TBFT primitives
	// (BLSSigner for value-signing, KyberSigner for IBE-tag signing under
	// the DST-trick approach, TLockIBE for layer encryption, plus the
	// pubkey-shares map and committee). See docs/IBE-INTEGRATION.md and
	// protocol/v2/ssv/runner/tbft for the adapter API. NewProposerRunner
	// returns an error when this is nil.
	TBFTController *tbftadapter.Controller
}

func NewProposerRunner(opts ProposerRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}
	if opts.TBFTController == nil {
		return nil, errors.New("TBFTController is required for ProposerRunner")
	}

	r := &ProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleProposer,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
			// QBFTController stays nil for the proposer — the QBFT
			// consensus path was removed in favor of TBFT. BaseRunner
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

	hooks := &tbftadapter.LifecycleHooks{
		FetchCandidate: r.tbftFetchCandidate,
		Broadcast:      r.tbftBroadcast,
		SubmitOutput:   r.tbftSubmitOutput,
		OnMissedSlot:   r.tbftOnMissedSlot,
	}
	sched, err := tbftadapter.NewScheduler(opts.TBFTController, hooks)
	if err != nil {
		return nil, fmt.Errorf("build TBFT scheduler: %w", err)
	}
	r.tbftCtrl = opts.TBFTController
	r.tbftSched = sched
	r.tbftRL = tbftadapter.NewRateLimiter()
	r.tbftSlots = make(map[phase0.Slot]*tbftSlotState)

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

	// Hand off to the TBFT driver. Each layer leader fetches via the
	// FetchCandidate hook at its own FetchAt offset; SubmitOutput
	// delivers the agreed-upon block to the beacon node. RANDAO is
	// plumbed via per-slot state so the FetchCandidate hook can use it.
	r.measurements.StartConsensus()
	return r.tbftStartSlot(ctx, logger, duty.Slot, fullSig)
}

// ProcessConsensus is unused on the proposer — TBFT carries its own
// envelope type (SSVTBFTMsgType, see proposer_tbft.go::ProcessTBFTEnvelopeMsg).
// QBFT consensus messages arriving at a proposer runner indicate a
// misrouted message or a peer running an older binary; we surface them
// as an error so the network layer logs and drops them.
func (r *ProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return fmt.Errorf("proposer runner: QBFT consensus messages are not handled (TBFT only)")
}

// ProcessPostConsensus is unused on the proposer — TBFT folds post-
// consensus aggregation into Phase 3 of the protocol, surfaced via the
// SubmitOutput hook (proposer_tbft.go::tbftSubmitOutput). Post-consensus
// partial-signature messages arriving at a proposer runner are treated
// the same way as stray QBFT consensus messages above.
func (r *ProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return fmt.Errorf("proposer runner: QBFT post-consensus messages are not handled (TBFT only)")
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
// but unused on the proposer — TBFT doesn't run a post-consensus partial-
// sig collection phase (the reconstructed block-root signature falls out
// of Phase 3's IBE walk, see proposer_tbft.go::tbftSubmitOutput).
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, phase0.DomainType{}, fmt.Errorf("proposer runner: no post-consensus phase (TBFT only)")
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

	// reset the cached original block at the beginning of a new duty
	r.cachedFullBlock = nil
	r.cachedBlindedBlockSSZ = nil

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
	// Runtime dependencies (TBFT controller, hooks, signers, ekm signer,
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
