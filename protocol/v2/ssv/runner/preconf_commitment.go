package runner

import (
	"context"
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ssz "github.com/ferranbt/fastssz"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
	"sync"
	"tailscale.com/util/singleflight"
	"time"
)

const (
	// childRunnerInitializationTimeout - TODO how long should we wait ?
	childRunnerInitializationTimeout = 60 * time.Second
)

type PreconfCommitmentResult struct {
	CommitmentSignature []byte
}

// pcRunner wraps BaseRunner providing means of
type pcRunner struct {
	// mtx synchronizes access to BaseRunner
	mtx sync.Mutex
	BaseRunner

	// initialized indicates if this pcRunner has been initialized, once this channel is
	// closed it means BaseRunner is ready to process preconf-commitment duty (preconf-related
	// messages received from other operators). Note, it is possible that initialized channel
	// is never closed (meaning pcRunner has never been initialized) - so the receiver needs
	// to handle that by timing out (instead of blocking forever)
	initialized chan struct{}

	// result will contain the execution result of pcRunner (success or error), it's a channel
	// so it can be read (and waited on) concurrently
	result chan PreconfCommitmentResult
}

type PreconfCommitmentRunner struct {
	// baseRunner is responsible for some basic stuff like signing
	baseRunner *BaseRunner

	// childRunnersInflight prevents concurrent requests to childRunners from initializing
	// the same childRunners entry twice (instead we'll initialize just 1 childRunner instance
	// and return that same instance on repetitive gets).
	childRunnersInflight singleflight.Group[[32]byte, *pcRunner]
	// childRunners maintains mapping between preconf sign-requests and corresponding runners
	// that process those requests. It maps signing-root (not the actual preconf object-root
	// but a root of a message that contains preconf object + signing domain). It's a ttlcache
	// so we can access it concurrently clean it up periodically (to prevent no longer relevant
	// runners from piling up).
	childRunners *ttlcache.Cache[[32]byte, *pcRunner]

	domainType spectypes.DomainType
	share      *spectypes.Share

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	// quorum defines how many operators a validator corresponding to this runner is split between
	quorum uint64
}

func NewPreconfCommitmentRunner(
	domainType spectypes.DomainType,
	share *spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	quorum uint64,
) (Runner, error) {
	r := &PreconfCommitmentRunner{
		baseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RolePreconfCommitment,
			DomainType:     domainType,
			BeaconNetwork:  beacon.GetBeaconNetwork(),
			Share: map[phase0.ValidatorIndex]*spectypes.Share{
				share.ValidatorIndex: share,
			},
		},
		childRunners: ttlcache.New(
			// TODO how long should child runners live for ? setting it to ~2 epochs for now
			ttlcache.WithTTL[[32]byte, *pcRunner](2 * 32 * 12 * time.Second),
		),
		domainType:     domainType,
		share:          share,
		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		quorum:         quorum,
	}

	go r.childRunners.Start()

	return r, nil
}

func (r *PreconfCommitmentRunner) StartNewDutyWithResponse(
	ctx context.Context,
	logger *zap.Logger,
	validatorIndex phase0.ValidatorIndex,
	objectRootHex string,
) (chan PreconfCommitmentResult, error) {
	objectRootRaw, err := hexutil.Decode(objectRootHex)
	if err != nil {
		return nil, fmt.Errorf("decode objectRootHex: %s, error: %w", objectRootHex, err)
	}
	if len(objectRootRaw) != 32 {
		return nil, errors.New("objectRootHex must be 32 bytes")
	}

	objectRoot := [32]byte(objectRootRaw)
	duty := spectypes.PreconfCommitmentDuty(objectRoot)

	logger = logger.With(
		zap.String("preconf-commitment runner", "starting duty"),
		zap.String("object_root", hexutil.Encode(objectRoot[:])),
	)

	// commit-boost uses DomainApplicationBuilder domain for signing, see this doc for details
	// https://github.com/Commit-Boost/commit-boost-client/blob/c5a16eec53b7e6ce0ee5c18295565f1a0aa6e389/docs/docs/developing/commit-module.md#requesting-signatures
	msg, err := r.baseRunner.signPreconfCommitment(
		r,
		validatorIndex,
		&duty,
		duty.DutySlot(),
		spectypes.DomainApplicationBuilder,
	)
	if err != nil {
		return nil, fmt.Errorf("could not sign preconf-commitment: %w", err)
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PreconfCommitmentPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}
	msgID := spectypes.NewMsgID(r.baseRunner.DomainType, r.GetShare().ValidatorPubKey[:], r.baseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign SSVMessage")
	}
	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	// TODO - which one it is ?
	//signingRoot, err := msg.GetRoot()
	//signingRoot, err := msgs.GetRoot()
	//signingRoot, err := msgToBroadcast.GetRoot()
	signingRoot, err := ssvMsg.HashTreeRoot()
	//signingRoot, err := duty.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("compute signing root: %w", err)
	}
	cRunner, err := r.childRunner(signingRoot, &duty)
	if err != nil {
		return nil, fmt.Errorf("failed to get child runner: %w", err)
	}
	close(cRunner.initialized)

	logger.Debug(
		"broadcasting partial sig",
		zap.String("signing_root", hexutil.Encode(signingRoot[:])),
	)
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return nil, fmt.Errorf("failed to broadcast partial signature: %w", err)
	}

	return cRunner.result, nil
}

func (r *PreconfCommitmentRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return fmt.Errorf("StartNewDuty method isn't supported")
}

// HasRunningDuty returns true if a duty is already running
func (r *PreconfCommitmentRunner) HasRunningDuty() bool {
	return true // assume always running, it doesn't matter much for preconf-commitment duty
}

func (r *PreconfCommitmentRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	signingRoot := signedMsg.Messages[0].SigningRoot
	if signingRoot == [32]byte{} {
		return fmt.Errorf("pre-consensus message has empty root")
	}

	logger = logger.With(
		zap.String("preconf-commitment runner", "processing pre-consensus message"),
		zap.String("signing_root", hexutil.Encode(signingRoot[:])),
	)

	duty := spectypes.PreconfCommitmentDuty(signingRoot)
	cRunner, err := r.childRunner(signingRoot, &duty)
	if err != nil {
		return fmt.Errorf("failed to get child runner: %w", err)
	}
	go func() {
		select {
		case <-cRunner.initialized:
		case <-time.After(childRunnerInitializationTimeout):
			// looks like this child-runner won't be initialized, terminating here to release
			// resources
			logger.Debug("timed out waiting for child runner to be initialized")
			return
		case <-ctx.Done():
			logger.Debug("context canceled waiting for child runner to be initialized")
			return
		}

		// child-runner has been initialized

		cRunner.mtx.Lock()
		defer cRunner.mtx.Unlock()

		quorum, roots, err := cRunner.basePreConsensusMsgProcessing(r, signedMsg)
		if err != nil {
			logger.Error("basePreConsensusMsgProcessing failed", zap.Error(err))
			return
		}

		logger.Debug(
			"got partial sig",
			zap.Uint64("signer", signedMsg.Messages[0].Signer),
			zap.Bool("quorum", quorum),
		)

		// quorum returns true only once (first time quorum achieved)
		if !quorum {
			return
		}

		// sanity checks
		if len(roots) != 1 {
			logger.Error("pre-consensus message has more than one root", zap.Int("roots", len(roots)))
			return
		}
		if roots[0] != signingRoot {
			logger.Error(fmt.Sprintf(
				"base runner extracted root %s that doesn't match pre-consensus message root %s",
				hexutil.Encode(roots[0][:]),
				hexutil.Encode(signingRoot[:]),
			))
			return
		}

		fullSig, err := cRunner.State.ReconstructBeaconSig(
			cRunner.State.PreConsensusContainer,
			signingRoot,
			r.share.ValidatorPubKey[:],
			r.share.ValidatorIndex,
		)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			cRunner.FallBackAndVerifyEachSignature(
				cRunner.State.PreConsensusContainer,
				signingRoot,
				r.share.Committee,
				r.share.ValidatorIndex,
			)
			logger.Error("got pre-consensus quorum but it has invalid signatures", zap.Error(err))
			return
		}

		logger.Debug(
			"preconf-commitment was signed successfully",
			zap.String("signature", hexutil.Encode(fullSig)),
		)
		cRunner.State.Finished = true
		cRunner.result <- PreconfCommitmentResult{
			CommitmentSignature: fullSig,
		}
	}()

	logger.Debug("spun up child runner")

	return nil
}

func (r *PreconfCommitmentRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for preconf-commitment")
}

func (r *PreconfCommitmentRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return errors.New("no qbft phase for preconf-commitment")
}

func (r *PreconfCommitmentRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for preconf-commitment")
}

func (r *PreconfCommitmentRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	// TODO - implement this properly for preconfs
	// compare hash-root that comes with pre-consensus message against what corresponding child-runner
	// expects based on the data it has pulled on its own
	// NOTE, currently with Bolt (or ETHgas) there is no way for us to fetch data about preconf(s)
	// independently: https://github.com/chainbound/bolt/issues/772
	return nil, spectypes.DomainApplicationBuilder, nil

	//if r.BaseRunner.State == nil || r.BaseRunner.State.StartingDuty == nil {
	//	return nil, spectypes.DomainError, errors.New("no running duty to compute preconsensus roots and domain")
	//}
	//vr, err := r.calculatePreconfCommitment(r.BaseRunner.State.StartingDuty.DutySlot())
	//if err != nil {
	//	return nil, spectypes.DomainError, errors.Wrap(err, "could not calculate preconf-commitment")
	//}
	//return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *PreconfCommitmentRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for preconf-commitment")
}

func (r *PreconfCommitmentRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	return fmt.Errorf("executeDuty method isn't supported")
}

func (r *PreconfCommitmentRunner) HasRunningQBFTInstance() bool {
	return false
}

func (r *PreconfCommitmentRunner) HasAcceptedProposalForCurrentRound() bool {
	return false
}

func (r *PreconfCommitmentRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	result := make(map[phase0.ValidatorIndex]*spectypes.Share, 1)
	result[r.share.ValidatorIndex] = r.share
	return result
}

func (r *PreconfCommitmentRunner) GetRole() spectypes.RunnerRole {
	return spectypes.RolePreconfCommitment
}

func (r *PreconfCommitmentRunner) GetLastHeight() specqbft.Height {
	return 0
}

func (r *PreconfCommitmentRunner) GetLastRound() specqbft.Round {
	return 0
}

func (r *PreconfCommitmentRunner) GetStateRoot() ([32]byte, error) {
	return [32]byte{}, fmt.Errorf("GetStateRoot not implemented")
}

func (r *PreconfCommitmentRunner) SetTimeoutFunc(fn TimeoutF) {}

func (r *PreconfCommitmentRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *PreconfCommitmentRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *PreconfCommitmentRunner) GetShare() *spectypes.Share {
	return r.share
}

func (r *PreconfCommitmentRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return nil
}

func (r *PreconfCommitmentRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}
func (r *PreconfCommitmentRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *PreconfCommitmentRunner) Encode() ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

// Decode returns error if decoding failed
func (r *PreconfCommitmentRunner) Decode(data []byte) error {
	return fmt.Errorf("not implemented")
}

// GetRoot returns the root used for signing and verification
func (r *PreconfCommitmentRunner) GetRoot() ([32]byte, error) {
	return [32]byte{}, fmt.Errorf("not implemented")
}

func (r *PreconfCommitmentRunner) childRunner(signingRoot [32]byte, duty *spectypes.PreconfCommitmentDuty) (*pcRunner, error) {
	result, err, _ := r.childRunnersInflight.Do(signingRoot, func() (*pcRunner, error) {
		item := r.childRunners.Get(signingRoot)
		if item != nil {
			return item.Value(), nil
		}

		result := pcRunner{
			BaseRunner: BaseRunner{
				RunnerRoleType: spectypes.RolePreconfCommitment,
				DomainType:     r.domainType,
				BeaconNetwork:  r.beacon.GetBeaconNetwork(),
				Share: map[phase0.ValidatorIndex]*spectypes.Share{
					r.share.ValidatorIndex: r.share,
				},
			},
			initialized: make(chan struct{}),
			result:      make(chan PreconfCommitmentResult),
		}
		result.BaseRunner.baseSetupForNewDuty(duty, r.quorum)

		r.childRunners.Set(signingRoot, &result, ttlcache.DefaultTTL)

		return &result, nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}
