package runner

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	// childRunnerInitializationTimeout - TODO how long should we wait ? setting it to ~2 epochs for now
	childRunnerInitializationTimeout = 2 * 32 * 12 * time.Second
)

type PreconfCommitmentResult struct {
	Success struct {
		CommitmentSignature []byte
	}
	Err error
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
	// childRunnersInflight prevents concurrent requests to childRunners from initializing
	// the same childRunners entry twice (instead we'll initialize just 1 childRunner instance
	// and return that same instance on repetitive gets).
	childRunnersInflight singleflight.Group[spectypes.PreconfCommitmentDuty, *pcRunner]
	// childRunners maintains mapping between preconf sign-requests and corresponding runners
	// that process those requests. It's a ttlcache so we can access it concurrently clean it up
	// periodically (to prevent no longer relevant runners from piling up).
	childRunners *ttlcache.Cache[spectypes.PreconfCommitmentDuty, *pcRunner]

	share *spectypes.Share

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	// quorum defines how many operators a validator corresponding to this runner is split between
	quorum uint64
}

func NewPreconfCommitmentRunner(
	share *spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	quorum uint64,
) (Runner, error) {
	r := &PreconfCommitmentRunner{
		childRunners: ttlcache.New(
			// TODO how long should child runners live for ? setting it to ~2 epochs for now
			ttlcache.WithTTL[spectypes.PreconfCommitmentDuty, *pcRunner](2 * 32 * 12 * time.Second),
		),
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
	objectRootRaw, err := hex.DecodeString(objectRootHex)
	if err != nil {
		return nil, fmt.Errorf("decode objectRootHex: %s, error: %w", objectRootHex, err)
	}
	if len(objectRootRaw) != 32 {
		return nil, errors.New("objectRootHex must be 32 bytes")
	}
	root := spectypes.PreconfCommitmentDuty(objectRootRaw)

	cRunner, err := r.childRunner(root)
	if err != nil {
		// TODO - can we even get an error from `r.childRunner(root)` ?
		return nil, fmt.Errorf("failed to get child runner: %w", err)
	}
	defer close(cRunner.initialized)

	// commit-boost uses DomainApplicationBuilder domain for signing, see this doc for details
	// https://github.com/Commit-Boost/commit-boost-client/blob/c5a16eec53b7e6ce0ee5c18295565f1a0aa6e389/docs/docs/developing/commit-module.md#requesting-signatures
	msg, err := cRunner.signPreconfCommitment(
		r,
		validatorIndex,
		spectypes.SSZBytes(root[:]),
		phase0.Slot(0), // TODO - what slot should we use here ?
		spectypes.DomainApplicationBuilder,
	)
	if err != nil {
		return nil, fmt.Errorf("could not sign preconf-commitment: %w", err)
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PreconfCommitmentPartialSig,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(cRunner.DomainType, r.GetShare().ValidatorPubKey[:], cRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return nil, err
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

	logger.Debug(
		"broadcasting preconf-commitment partial sig",
		zap.Any("preconf_commitment_root", root),
	)

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return nil, fmt.Errorf("failed to broadcast preconf-commitment partial signature: %w", err)
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
	root := signedMsg.Messages[0].SigningRoot
	if root == [32]byte{} {
		return fmt.Errorf("pre-consensus message has empty root")
	}

	logger = logger.With(
		zap.String("preconf_commitment_runner", "process pre-consensus message"),
		zap.String("root", hex.EncodeToString(root[:])),
	)

	cRunner, err := r.childRunner(root)
	if err != nil {
		// TODO - can we even get an error from `r.childRunner(root)` ?
		return fmt.Errorf("failed to get child runner: %w", err)
	}
	go func() {
		select {
		case <-cRunner.initialized:
		case <-time.After(childRunnerInitializationTimeout):
			// looks like this child-runner won't be initialized, terminating here to release
			// resources
			return
		case <-ctx.Done():
			return
		}

		// child-runner has been initialized

		cRunner.mtx.Lock()
		defer cRunner.mtx.Unlock()

		quorum, roots, err := cRunner.basePreConsensusMsgProcessing(r, signedMsg)
		if err != nil {
			cRunner.result <- PreconfCommitmentResult{
				Err: fmt.Errorf("basePreConsensusMsgProcessing: %w", err),
			}
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
			cRunner.result <- PreconfCommitmentResult{
				Err: fmt.Errorf("pre-consensus message has more than one root (%d)", len(roots)),
			}
			return
		}
		if roots[0] != root {
			cRunner.result <- PreconfCommitmentResult{
				Err: fmt.Errorf("base runner extracted root %s that doesn't match pre-consensus message root %s", roots[0], root),
			}
			return
		}

		fullSig, err := cRunner.State.ReconstructBeaconSig(
			cRunner.State.PreConsensusContainer,
			root,
			r.share.ValidatorPubKey[:],
			r.share.ValidatorIndex,
		)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			cRunner.FallBackAndVerifyEachSignature(
				cRunner.State.PreConsensusContainer,
				root,
				r.share.Committee,
				r.share.ValidatorIndex,
			)
			cRunner.result <- PreconfCommitmentResult{
				Err: fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err),
			}
		}
		// TODO - do we need array here ?
		//specSig := phase0.BLSSignature{}
		//copy(specSig[:], fullSig)

		logger.Debug(
			"preconf-commitment was signed successfully",
			zap.String("signature", hex.EncodeToString(fullSig)),
		)
		cRunner.State.Finished = true
		cRunner.result <- PreconfCommitmentResult{
			Success: struct{ CommitmentSignature []byte }{CommitmentSignature: fullSig},
		}
	}()

	logger.Debug("spun up preconf-commitment child runner to process pre-consensus message")

	return nil
}

func (r *PreconfCommitmentRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for preconf-commitment")
}

func (r *PreconfCommitmentRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return errors.New("no qbft phase")
}

func (r *PreconfCommitmentRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for preconf-commitment")
}

func (r *PreconfCommitmentRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	// TODO - implement this for preconfs
	// compare hash-root that comes with pre-consensus message against what corresponding child-runner
	// expects based on the data it has pulled on its own
	// NOTE, currently with Bolt there is no way for us to fetch data about preconf(s) independently:
	// https://github.com/chainbound/bolt/issues/772
	return nil, phase0.DomainType{}, nil

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

func (r *PreconfCommitmentRunner) childRunner(root spectypes.PreconfCommitmentDuty) (*pcRunner, error) {
	result, err, _ := r.childRunnersInflight.Do(root, func() (*pcRunner, error) {
		item := r.childRunners.Get(root)
		if item != nil {
			return item.Value(), nil
		}

		result := pcRunner{
			BaseRunner: BaseRunner{
				// TODO - do we need to initialize RunnerRoleType on BaseRunner ?
				//RunnerRoleType: spectypes.RolePreconfCommitment,
				// TODO - do we need to initialize DomainType on BaseRunner ?
				//DomainType:     r.domainType,
				BeaconNetwork: r.beacon.GetBeaconNetwork(),
				// TODO - do we need to initialize Share on BaseRunner ?
				//Share:          r.share,
			},
			initialized: make(chan struct{}),
			result:      make(chan PreconfCommitmentResult),
		}
		// preconf-commitment BaseRunner doesn't use spectypes.Duty hence we pass nil
		result.BaseRunner.baseSetupForNewDuty(nil, r.quorum)

		r.childRunners.Set(root, &result, ttlcache.DefaultTTL)

		return &result, nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}
