package runner

import (
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type Getters interface {
	GetBaseRunner() *BaseRunner
	GetBeaconNode() beacon.BeaconNode
	GetValCheckF() specqbft.ProposedValueCheckF
	GetSigner() spectypes.BeaconSigner
	GetOperatorSigner() ssvtypes.OperatorSigner
	GetNetwork() specqbft.Network
}

type Runner interface {
	spectypes.Encoder
	spectypes.Root
	Getters

	// StartNewDuty starts a new duty for the runner, returns error if can't
	StartNewDuty(logger *zap.Logger, duty spectypes.Duty, quorum uint64) error
	// HasRunningDuty returns true if it has a running duty
	HasRunningDuty() bool
	// ProcessPreConsensus processes all pre-consensus msgs, returns error if can't process
	ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error
	// ProcessConsensus processes all consensus msgs, returns error if can't process
	ProcessConsensus(logger *zap.Logger, msg *spectypes.SignedSSVMessage) error
	// ProcessPostConsensus processes all post-consensus msgs, returns error if can't process
	ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error
	// expectedPreConsensusRootsAndDomain an INTERNAL function, returns the expected pre-consensus roots to sign
	expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error)
	// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
	expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error)
	// executeDuty an INTERNAL function, executes a duty.
	executeDuty(logger *zap.Logger, duty spectypes.Duty) error
}

var _ Runner = new(CommitteeRunner)

// BaseRunner contains base functionality for running duties.
// BaseRunner is not thread-safe and is meant to process each duty it is responsible for in
// sequential manner (one duty after another, and for any particular duty - one duty-related
// operation/event/message after another), this is mostly because it expects to apply DutyState
// updates sequentially (verifying the order of these updates makes sense, rejecting invalid
// state transitions).
type BaseRunner struct {
	// State represents runner state for the current duty. Every duty is expected to undergo
	// full duty processing cycle (pre-consensus, consensus and post-consensus phases), and
	// runner will modify & read State in serialized thread-safe manner - both when handling
	// events modifying State of currently running duty and events kicking off the start of
	// next duty to execute (in which case State if fully replaced by new State object) -
	// hence no mutex to protect State modifications is required.
	State *DutyState

	Shares         map[phase0.ValidatorIndex]*spectypes.Share
	QBFTController *controller.Controller
	DomainType     spectypes.DomainType
	BeaconNetwork  spectypes.BeaconNetwork
	RunnerRoleType spectypes.RunnerRole
	ssvtypes.OperatorSigner

	// implementation vars
	TimeoutF TimeoutF `json:"-"`

	// highestDecidedSlot holds the highest decided duty slot and gets updated after each decided is reached
	highestDecidedSlot phase0.Slot
}

func (b *BaseRunner) Encode() ([]byte, error) {
	return json.Marshal(b)
}

func (b *BaseRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &b)
}

func (b *BaseRunner) MarshalJSON() ([]byte, error) {
	type BaseRunnerAlias struct {
		State              *DutyState
		Share              map[phase0.ValidatorIndex]*spectypes.Share
		QBFTController     *controller.Controller
		BeaconNetwork      spectypes.BeaconNetwork
		RunnerRoleType     spectypes.RunnerRole
		highestDecidedSlot phase0.Slot
	}

	// Create object and marshal
	alias := &BaseRunnerAlias{
		State:              b.State,
		Share:              b.Shares,
		QBFTController:     b.QBFTController,
		BeaconNetwork:      b.BeaconNetwork,
		RunnerRoleType:     b.RunnerRoleType,
		highestDecidedSlot: b.highestDecidedSlot,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

// SetHighestDecidedSlot set highestDecidedSlot for base runner
func (b *BaseRunner) SetHighestDecidedSlot(slot phase0.Slot) {
	b.highestDecidedSlot = slot
}

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(logger *zap.Logger, runner Runner, duty spectypes.Duty, quorum uint64) error {
	if err := b.ShouldProcessDuty(duty); err != nil {
		return errors.Wrap(err, "can't start duty")
	}
	b.State = NewDutyState(quorum, duty)
	return runner.executeDuty(logger, duty)
}

// baseStartNewBeaconDuty is a base func that all runner implementation can call to start a non-beacon duty
func (b *BaseRunner) baseStartNewNonBeaconDuty(logger *zap.Logger, runner Runner, duty *spectypes.ValidatorDuty, quorum uint64) error {
	if err := b.ShouldProcessNonBeaconDuty(duty); err != nil {
		return errors.Wrap(err, "can't start non-beacon duty")
	}
	b.State = NewDutyState(quorum, duty)
	return runner.executeDuty(logger, duty)
}

// basePreConsensusMsgProcessing is a base func that all runner implementation can call for processing a pre-consensus msg
func (b *BaseRunner) basePreConsensusMsgProcessing(runner Runner, signedMsg *spectypes.PartialSignatureMessages) (bool, [][32]byte, error) {
	if err := b.ValidatePreConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid pre-consensus message")
	}

	hasQuorum, roots := b.basePartialSigMsgProcessing(signedMsg, b.State.PreConsensusContainer)
	return hasQuorum, roots, nil
}

// processConsensusMsg is a base func that all runner implementation can call for processing
// a QBFT consensus msg, it returns true only the very first time we've received and
// successfully processed deciding QBFT consensus message (in which case it's value will be
// written to decidedValueDecoder that defines the type of decided message as well as encoder
// to use when reconstructing decided value from msg payload).
func (b *BaseRunner) processConsensusMsg(
	logger *zap.Logger,
	runner Runner,
	msg *spectypes.SignedSSVMessage,
	decidedValueDecoder spectypes.Encoder,
) (decided bool, err error) {
	// TODO - these "strict" checks will not work super well in practice until the following
	// issue is resolved: https://github.com/ssvlabs/ssv/issues/1827
	if !b.hasRunningDuty() {
		return false, fmt.Errorf("can't process consensus message, no running duty")
	}
	if !b.hasRunningInstance() {
		return false, fmt.Errorf("can't process consensus message, no running QBFT instance")
	}
	alreadyDecided, _ := b.State.QBFTInstance.IsDecided()
	if alreadyDecided {
		return false, fmt.Errorf("consensus has already finished")
	}
	// TODO ^ instead, we probably want to handle different possibilities here in different ways:
	// - return error signaling Duty is not running yet (the caller needs to handle this error by
	//   "buffering" this msg, re-trying processConsensusMsg for this msg again later on)
	// - return error signaling Duty is done already (the caller needs to handle this error by
	//   simply dropping this msg - no need to log it, it's expected situation to be in)
	// - return error signaling QBFT Instance is not running yet (the caller needs to handle
	//   this error by "buffering" this msg, re-trying processConsensusMsg for this msg again
	//   later on)
	// - return error signaling QBFT Instance is done already (the caller needs to handle this
	//   error by simply dropping this msg - no need to log it, it's expected situation to be in)
	// To implement these ^ properly we'll need to compare message height against QBFTInstance
	// to understand what QBFTInstance this msg is targeting - it might not exist yet, be
	// currently running or has already finished. Note however, QBFTController.ProcessMsg also
	// does check for future messages - perhaps we want to move that check here, it also claims
	// "all future messages will be saved into message container ..." - see how it plays into
	// what we are trying to implement here.
	//
	// Also, similar handling is needed for pre/post QBFT consensus phase.
	// Also, check/see how it fits into solution proposed & implemented in
	// https://github.com/ssvlabs/ssv/issues/1827

	decidedMsg, err := b.QBFTController.ProcessMsg(logger, msg)
	if err != nil {
		return false, fmt.Errorf("process consensus message: %w", err)
	}
	if decidedMsg == nil {
		// this isn't a decided message yet, or we have already acted on decided message
		// some time in the past once - won't be acting on it again
		return false, nil
	}

	// got decided message for the very first time then, lets process it

	if err := b.checkDecidedMsg(decidedMsg); err != nil {
		return true, fmt.Errorf("bad decided message: %w", err)
	}

	if err := decidedValueDecoder.Decode(decidedMsg.FullData); err != nil {
		return true, errors.Wrap(err, "failed to parse decided value to ValidatorConsensusData")
	}
	if err := b.validateDecidedConsensusData(runner, decidedValueDecoder); err != nil {
		return true, errors.Wrap(err, "decided ValidatorConsensusData invalid")
	}

	// update the highest decided slot
	b.highestDecidedSlot = b.State.StartingDuty.DutySlot()

	return true, nil
}

// basePostConsensusMsgProcessing is a base func that all runner implementation can call for processing a post-consensus msg
func (b *BaseRunner) basePostConsensusMsgProcessing(logger *zap.Logger, runner Runner, signedMsg *spectypes.PartialSignatureMessages) (bool, [][32]byte, error) {
	if err := b.ValidatePostConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid post-consensus message")
	}

	hasQuorum, roots := b.basePartialSigMsgProcessing(signedMsg, b.State.PostConsensusContainer)
	return hasQuorum, roots, nil
}

// basePartialSigMsgProcessing adds a validated (without signature verification) validated partial msg to the container, checks for quorum and returns true (and roots) if quorum exists
func (b *BaseRunner) basePartialSigMsgProcessing(
	signedMsg *spectypes.PartialSignatureMessages,
	container *ssv.PartialSigContainer,
) (bool, [][32]byte) {
	roots := make([][32]byte, 0)
	anyQuorum := false

	for _, msg := range signedMsg.Messages {
		prevQuorum := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		// Check if it has two signatures for the same signer
		if container.HasSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			b.resolveDuplicateSignature(container, msg)
		} else {
			container.AddSignature(msg)
		}

		hasQuorum := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		if hasQuorum && !prevQuorum {
			// Notify about first quorum only
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots
}

// checkDecidedMsg returns error if the consensus message is not a valid decided message or
// if it is a deciding message that's inconsistent with currently running QBFT instance
// state.
func (b *BaseRunner) checkDecidedMsg(signedMessage *spectypes.SignedSSVMessage) error {
	if signedMessage == nil {
		return fmt.Errorf("SignedSSVMessage message is nil")
	}
	if signedMessage.SSVMessage == nil {
		return fmt.Errorf("SSVMessage message is nil")
	}

	decidedMessage, err := specqbft.DecodeMessage(signedMessage.SSVMessage.Data)
	if err != nil {
		return fmt.Errorf("decode SSVMessage message: %w", err)
	}
	if decidedMessage == nil {
		return fmt.Errorf("decoded SSVMessage message is nil")
	}
	if decidedMessage.Height != b.State.QBFTInstance.GetHeight() {
		return fmt.Errorf(
			"wrong instance, decided message height: %d, running instance height: %d",
			decidedMessage.Height,
			b.State.QBFTInstance.GetHeight(),
		)
	}

	return nil
}

func (b *BaseRunner) decide(logger *zap.Logger, runner Runner, slot phase0.Slot, input spectypes.Encoder) error {
	byts, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode input data for consensus")
	}

	if err := runner.GetValCheckF()(byts); err != nil {
		return errors.Wrap(err, "input data invalid")
	}

	if err := runner.GetBaseRunner().QBFTController.StartNewInstance(logger,
		specqbft.Height(slot),
		byts,
	); err != nil {
		return errors.Wrap(err, "could not start new QBFT instance")
	}
	newInstance := runner.GetBaseRunner().QBFTController.StoredInstances.FindInstance(runner.GetBaseRunner().QBFTController.Height)
	if newInstance == nil {
		return errors.New("could not find newly created QBFT instance")
	}

	runner.GetBaseRunner().State.QBFTInstance = newInstance

	b.registerTimeoutHandler(logger, newInstance, runner.GetBaseRunner().QBFTController.Height)

	return nil
}

func (b *BaseRunner) hasRunningDuty() bool {
	return b.State != nil && !b.State.Finished
}

func (b *BaseRunner) hasRunningInstance() bool {
	return b.State != nil && b.State.QBFTInstance != nil
}

func (b *BaseRunner) ShouldProcessDuty(duty spectypes.Duty) error {
	if b.QBFTController.Height >= specqbft.Height(duty.DutySlot()) && b.QBFTController.Height != 0 {
		return errors.Errorf("duty for slot %d already passed. Current height is %d", duty.DutySlot(),
			b.QBFTController.Height)
	}
	return nil
}

func (b *BaseRunner) ShouldProcessNonBeaconDuty(duty spectypes.Duty) error {
	// assume StartingDuty is not nil if state is not nil
	if b.State != nil && b.State.StartingDuty.DutySlot() >= duty.DutySlot() {
		return errors.Errorf("duty for slot %d already passed. Current slot is %d", duty.DutySlot(),
			b.State.StartingDuty.DutySlot())
	}
	return nil
}
