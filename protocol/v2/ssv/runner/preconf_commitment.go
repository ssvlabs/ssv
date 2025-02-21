package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type rootHash [32]byte

type PreconfCommitmentResult struct {
	Success struct {
		CommitmentSignature []byte
	}
	Err error
}

// childRunner wraps BaseRunner providing means of
type childRunner struct {
	// mtx synchronizes access to BaseRunner
	mtx sync.Mutex
	BaseRunner

	// initialized indicates if this childRunner has been initialized, once channel is closed it
	// means its baseRunner is ready to process preconf-commitment duty (preconf-related messages
	// received from peer operators); close(initialized) might never happen though - so receiver
	// needs to handle that with timing out
	initialized chan struct{}

	// result will contain the execution result of childRunner (success or error), it's a channel
	// so it can be read (and waited on) concurrently
	result chan PreconfCommitmentResult
}

type PreconfCommitmentRunner struct {
	// childRunnersInflight prevents concurrent requests to childRunners from initializing
	// the same childRunners entry twice (instead we'll initialize just 1 childRunner instance
	// and return that same instance or repetitive gets).
	childRunnersInflight singleflight.Group[rootHash, *childRunner]
	// childRunners maintains mapping between preconf sign-requests (identified by rootHash) and
	// corresponding runners that process those requests. It's a ttlcache so we can access it
	// concurrently clean it up periodically (to prevent no longer relevant runners from piling up).
	childRunners *ttlcache.Cache[rootHash, *childRunner]

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	quorum         uint64
}

func NewPreconfCommitmentRunner(
	share map[phase0.ValidatorIndex]*spectypes.Share,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &PreconfCommitmentRunner{
		TODO: TODO,
		TODO: TODO,

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		quorum:         quorum,
	}, nil

	// TODO - how long will Runner be kept around ? Maybe 2 epochs is a good starting value
	//childRunners.Start()
}

func (r *PreconfCommitmentRunner) StartNewDutyWithResponse(
	ctx context.Context,
	logger *zap.Logger,
	validatorIndex phase0.ValidatorIndex,
	root rootHash,
) (chan PreconfCommitmentResult, error) {
	// TODO

	// TODO - how do we pass root here ? Maybe by casting duty to a special preconf-dedicated type
	// that has both `root` and `ValidatorIndex` ?
	cRunner, err := r.childRunner(root)
	if err != nil {
		// TODO - can we even get an error from `r.childRunner(root)` ?
		return nil, fmt.Errorf("failed to get child runner: %w", err)
	}
	defer close(cRunner.initialized)

	msg, err := cRunner.signPreconfCommitment(r, validatorIndex, root)
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
				Err: fmt.Errorf("base runner extracted root %s doesn't match pre-consensus message root %s", roots[0], root),
			}
			return
		}

		fullSig, err := cRunner.State.ReconstructBeaconSig(
			cRunner.State.PreConsensusContainer,
			root,
			cRunner.GetShare().ValidatorPubKey[:],
			cRunner.GetShare().ValidatorIndex,
		)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			cRunner.FallBackAndVerifyEachSignature(
				cRunner.State.PreConsensusContainer,
				root,
				cRunner.GetShare().Committee,
				cRunner.GetShare().ValidatorIndex,
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
		cRunner.result <- PreconfCommitmentResult{
			Success: struct{ CommitmentSignature []byte }{CommitmentSignature: fullSig},
		}
		cRunner.State.Finished = true
	}()

	logger.Debug("spun up child runner")

	return nil
}

func (r *PreconfCommitmentRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for validator registration")
}

func (r *PreconfCommitmentRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post consensus phase for validator registration")
}

func (r *PreconfCommitmentRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	// TODO - implement this for preconfs
	// compare hash-root that comes with pre-consensus message against what corresponding child-runner
	// expects based on the data it has pulled on its own
	// NOTE, currently with Bolt there is no way for us to fetch data about preconf(s) independently:
	// https://github.com/chainbound/bolt/issues/772

	return TODO, TODO, TODO

	//if r.BaseRunner.State == nil || r.BaseRunner.State.StartingDuty == nil {
	//	return nil, spectypes.DomainError, errors.New("no running duty to compute preconsensus roots and domain")
	//}
	//vr, err := r.calculatePreconfCommitment(r.BaseRunner.State.StartingDuty.DutySlot())
	//if err != nil {
	//	return nil, spectypes.DomainError, errors.Wrap(err, "could not calculate validator registration")
	//}
	//return []ssz.HashRoot{vr}, spectypes.DomainApplicationBuilder, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *PreconfCommitmentRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for validator registration")
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
	return r.BaseRunner.GetShares()
}

func (r *PreconfCommitmentRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
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

func (r *PreconfCommitmentRunner) SetTimeoutFunc(fn TimeoutF) {
	return
}

func (r *PreconfCommitmentRunner) GetNetwork() specqbft.Network {
	return nil
}

func (r *PreconfCommitmentRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *PreconfCommitmentRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *PreconfCommitmentRunner) GetState() *State {
	return r.BaseRunner.State
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
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *PreconfCommitmentRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *PreconfCommitmentRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode PreconfCommitmentRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (r *PreconfCommitmentRunner) childRunner(root rootHash) (*childRunner, error) {
	result, err, _ := r.childRunnersInflight.Do(root, func() (*childRunner, error) {
		item := r.childRunners.Get(root)
		if item != nil {
			return item.Value(), nil
		}

		result := childRunner{
			BaseRunner: BaseRunner{
				RunnerRoleType: spectypes.RolePreconfCommitment,
				DomainType:     r.domainType,
				BeaconNetwork:  r.beaconNetwork,
				Share:          r.share,
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
