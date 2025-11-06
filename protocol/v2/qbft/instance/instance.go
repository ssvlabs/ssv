package instance

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *specqbft.State
	config qbft.IConfig
	signer ssvtypes.OperatorSigner

	processMsgF *spectypes.ThreadSafeF

	forceStop    bool
	StartValue   []byte
	ValueChecker ssv.ValueChecker `json:"-"`

	metrics *metricsRecorder
}

func NewInstance(
	config qbft.IConfig,
	committeeMember *spectypes.CommitteeMember,
	identifier []byte,
	height specqbft.Height,
	signer ssvtypes.OperatorSigner,
) *Instance {
	runnerRole := spectypes.RunnerRole(spectypes.RoleUnknown) // RoleUnknown is of int type, hence have to type-cast
	if len(identifier) == 56 {
		runnerRole = spectypes.MessageID(identifier).GetRoleType()
	}

	return &Instance{
		State: &specqbft.State{
			CommitteeMember:      committeeMember,
			ID:                   identifier,
			Round:                specqbft.FirstRound,
			Height:               height,
			LastPreparedRound:    specqbft.NoRound,
			ProposeContainer:     specqbft.NewMsgContainer(),
			PrepareContainer:     specqbft.NewMsgContainer(),
			CommitContainer:      specqbft.NewMsgContainer(),
			RoundChangeContainer: specqbft.NewMsgContainer(),
		},
		config:      config,
		signer:      signer,
		processMsgF: spectypes.NewThreadSafeF(),
		metrics:     newMetrics(runnerRole),
	}
}

func (i *Instance) ForceStop() {
	i.forceStop = true
}

// Start is an interface implementation
func (i *Instance) Start(
	ctx context.Context,
	logger *zap.Logger,
	value []byte,
	height specqbft.Height,
	valueChecker ssv.ValueChecker,
) {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "qbft.instance.start"),
		trace.WithAttributes(observability.BeaconSlotAttribute(phase0.Slot(height))))
	defer span.End()

	logger = logger.With(fields.QBFTRound(specqbft.FirstRound), fields.QBFTHeight(height))

	proposerID := i.ProposerForRound(specqbft.FirstRound)

	const startingQBFTInstanceEvent = "ℹ️ starting QBFT instance"
	logger.Debug(
		startingQBFTInstanceEvent,
		zap.Uint64("us", i.State.CommitteeMember.OperatorID),
		zap.Uint64("leader", proposerID),
	)
	span.AddEvent(startingQBFTInstanceEvent, trace.WithAttributes(observability.ValidatorProposerAttribute(proposerID)))

	i.StartValue = value
	i.bumpToRound(specqbft.FirstRound)
	i.State.Height = height
	i.ValueChecker = valueChecker
	i.metrics.Start()
	i.config.GetTimer().TimeoutForRound(height, specqbft.FirstRound)

	// propose if this node is the proposer
	if proposerID == i.State.CommitteeMember.OperatorID {
		proposal, err := i.CreateProposal(i.StartValue, nil, nil)
		if err != nil {
			logger.Warn("❗ failed to create proposal", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
			// TODO align spec to add else to avoid broadcast errored proposal
		}

		r, err := specqbft.HashDataRoot(i.StartValue) // TODO (better than decoding?)
		if err != nil {
			logger.Warn("❗ failed to hash input data", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}

		logger = logger.With(fields.Root(r))
		const eventMsg = "📢 leader broadcasting proposal message"
		logger.Debug(eventMsg)
		span.AddEvent(eventMsg, trace.WithAttributes(attribute.String("root", hex.EncodeToString(r[:]))))

		if err := i.Broadcast(proposal); err != nil {
			logger.Warn("❌ failed to broadcast proposal", zap.Error(err))
			span.RecordError(err)
		}
	}

	span.SetStatus(codes.Ok, "")
}

func (i *Instance) Broadcast(msg *spectypes.SignedSSVMessage) error {
	if !i.CanProcessMessages() {
		return spectypes.NewError(spectypes.InstanceStoppedProcessingMessagesErrorCode, "instance stopped processing messages")
	}

	return i.GetConfig().GetNetwork().Broadcast(msg.SSVMessage.GetID(), msg)
}

func allSigners(all []*specqbft.ProcessingMessage) []spectypes.OperatorID {
	signers := make([]spectypes.OperatorID, 0, len(all))
	for _, m := range all {
		signers = append(signers, m.SignedMessage.OperatorIDs...)
	}
	return signers
}

// ProcessMsg processes a new QBFT msg, returns non nil error on msg processing error
func (i *Instance) ProcessMsg(ctx context.Context, logger *zap.Logger, msg *specqbft.ProcessingMessage) (decided bool, decidedValue []byte, aggregatedCommit *spectypes.SignedSSVMessage, err error) {
	if !i.CanProcessMessages() {
		return false, nil, nil, spectypes.NewError(spectypes.InstanceStoppedProcessingMessagesErrorCode, "instance stopped processing messages")
	}

	if err := i.BaseMsgValidation(msg); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	res := i.processMsgF.Run(func() interface{} {
		switch msg.QBFTMessage.MsgType {
		case specqbft.ProposalMsgType:
			return i.uponProposal(ctx, logger, msg)
		case specqbft.PrepareMsgType:
			return i.uponPrepare(ctx, logger, msg)
		case specqbft.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.UponCommit(ctx, logger, msg)
			if decided {
				i.State.Decided = decided
				i.State.DecidedValue = decidedValue
			}
			return err
		case specqbft.RoundChangeMsgType:
			return i.uponRoundChange(ctx, logger, msg)
		default:
			return errors.New("signed message type not supported")
		}
	})
	if res != nil {
		return false, nil, nil, res.(error)
	}
	return i.State.Decided, i.State.DecidedValue, aggregatedCommit, nil
}

func (i *Instance) BaseMsgValidation(msg *specqbft.ProcessingMessage) error {
	if err := msg.Validate(); err != nil {
		return err
	}

	// If a node gets a commit quorum before round change and other nodes don't,
	// then the other nodes wouldn't be able to get the commit quorum,
	// unless we allow decided messages from previous round.
	decided := msg.QBFTMessage.MsgType == specqbft.CommitMsgType && i.State.CommitteeMember.HasQuorum(len(msg.SignedMessage.OperatorIDs))
	if !decided && msg.QBFTMessage.Round < i.State.Round {
		return spectypes.NewError(spectypes.PastRoundErrorCode, "past round")
	}

	switch msg.QBFTMessage.MsgType {
	case specqbft.ProposalMsgType:
		return i.isValidProposal(msg)
	case specqbft.PrepareMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return NewRetryableError(spectypes.WrapError(spectypes.NoProposalForCurrentRoundErrorCode, ErrNoProposalForCurrentRound))
		}

		return i.validSignedPrepareForHeightRoundAndRootIgnoreSignature(
			msg,
			i.State.Round,
			proposedMsg.QBFTMessage.Root,
		)
	case specqbft.CommitMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return NewRetryableError(spectypes.WrapError(spectypes.NoProposalForCurrentRoundErrorCode, ErrNoProposalForCurrentRound))
		}
		return i.validateCommit(msg)
	case specqbft.RoundChangeMsgType:
		return i.validRoundChangeForDataIgnoreSignature(msg, msg.QBFTMessage.Round, msg.SignedMessage.FullData)
	default:
		return errors.New("signed message type not supported")
	}
}

// IsDecided interface implementation
func (i *Instance) IsDecided() (bool, []byte) {
	if state := i.State; state != nil {
		return state.Decided, state.DecidedValue
	}
	return false, nil
}

// GetConfig returns the instance config
func (i *Instance) GetConfig() qbft.IConfig {
	return i.config
}

// SetConfig returns the instance config
func (i *Instance) SetConfig(config qbft.IConfig) {
	i.config = config
}

// GetHeight interface implementation
func (i *Instance) GetHeight() specqbft.Height {
	return i.State.Height
}

// GetRoot returns the state's deterministic root
func (i *Instance) GetRoot() ([32]byte, error) {
	return i.State.GetRoot()
}

// Encode implementation
func (i *Instance) Encode() ([]byte, error) {
	return json.Marshal(i)
}

// Decode implementation
func (i *Instance) Decode(data []byte) error {
	return json.Unmarshal(data, &i)
}

// bumpToRound sets round and sends current round metrics.
func (i *Instance) bumpToRound(round specqbft.Round) {
	i.State.Round = round
}

// CanProcessMessages will return true if instance can process messages
func (i *Instance) CanProcessMessages() bool {
	return !i.forceStop && i.State.Round < i.config.GetCutOffRound()
}
