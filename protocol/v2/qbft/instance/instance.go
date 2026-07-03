package instance

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// Instance represents a single QBFT instance. It is NOT thread-safe.
type Instance struct {
	logger *zap.Logger

	config qbft.IConfig
	signer ssvtypes.OperatorSigner

	State        *specqbft.State
	processMsgF  *spectypes.ThreadSafeF
	StartValue   []byte
	ValueChecker ssv.ValueChecker `json:"-"`
	roundTimer   ssv.QBFTRoundTimer
	// markedIrrelevant is set to signal that Instance will no longer process messages (aka forcefully stopped in
	// ssv-spec terms).
	markedIrrelevant bool

	metrics *metricsRecorder
}

func NewInstance(
	ctx context.Context,
	logger *zap.Logger,
	config qbft.IConfig,
	committeeMember *spectypes.CommitteeMember,
	identifier []byte,
	height specqbft.Height,
	signer ssvtypes.OperatorSigner,
	roundTimerF ssv.QBFTRoundTimerF,
) *Instance {
	runnerRole := spectypes.RoleUnknown
	if len(identifier) == 56 {
		runnerRole = spectypes.MessageID(identifier).GetRoleType()
	}

	logger = logger.Named(log.NameQBFTInstance)

	return &Instance{
		logger: logger,
		config: config,
		signer: signer,
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
		processMsgF: spectypes.NewThreadSafeF(),
		roundTimer:  roundTimerF(ctx, logger, phase0.Slot(height)),
		metrics:     newMetrics(logger, runnerRole),
	}
}

// Timer returns the instance timer.
func (i *Instance) Timer() specqbft.Timer {
	return i.roundTimer
}

func (i *Instance) Start(
	ctx context.Context,
	value []byte,
	valueChecker ssv.ValueChecker,
) {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "qbft.instance.start"),
		trace.WithAttributes(observability.BeaconSlotAttribute(phase0.Slot(i.State.Height))))
	defer span.End()

	logger := i.logger.With(fields.QBFTRound(specqbft.FirstRound), fields.QBFTHeight(i.State.Height))

	proposerID := i.ProposerForRound(specqbft.FirstRound)

	const startingQBFTInstanceEvent = "ℹ️ starting QBFT instance"
	logger.Debug(
		startingQBFTInstanceEvent,
		zap.Uint64("us", i.State.CommitteeMember.OperatorID),
		zap.Uint64("leader", proposerID),
	)
	span.AddEvent(startingQBFTInstanceEvent, trace.WithAttributes(observability.ValidatorProposerAttribute(proposerID)))

	i.StartValue = value
	i.ValueChecker = valueChecker
	i.roundTimer.TimeoutForRound(specqbft.FirstRound)
	i.metrics.StartStage(stageProposal)

	// propose if this node is the proposer
	if proposerID == i.State.CommitteeMember.OperatorID {
		proposal, err := i.CreateProposal(i.StartValue, nil, nil)
		if err != nil {
			logger.Warn("❗ failed to create proposal", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
			// TODO align spec to add else to avoid broadcast errored proposal
		}

		startValueRoot := qbft.HashDataRoot(i.StartValue)
		logger = logger.With(zap.String("qbft_start_value_root", hex.EncodeToString(startValueRoot[:])))

		const eventMsg = "📢 leader broadcasting proposal message"
		logger.Debug(eventMsg)
		span.AddEvent(eventMsg, trace.WithAttributes(attribute.String("qbft_start_value_root", hex.EncodeToString(startValueRoot[:]))))

		if err := i.Broadcast(proposal); err != nil {
			logger.Warn("❌ failed to broadcast proposal", zap.Error(err))
			span.RecordError(err)
		}
	}

	span.SetStatus(codes.Ok, "")
}

// MarkDecided marks instance as decided, recording the decided-round and decided-value.
// This func essentially terminates instance (no QBFT-related progress is done afterward), releasing all the resources
// it spawned.
// Both MarkDecided and MarkIrrelevant can be called on the same instance, these calls do not conflict.
func (i *Instance) MarkDecided(round specqbft.Round, value []byte) error {
	if i.State.Decided {
		return fmt.Errorf(
			"instance has already decided in round %d (attempted to mark as decided in round %d)",
			i.State.Round,
			round,
		)
	}
	i.State.Decided = true
	i.State.Round = round
	i.State.DecidedValue = value
	i.roundTimer.Stop()
	return nil
}

// MarkIrrelevant marks instance as irrelevant to signal that it will no longer process messages, hence no further
// progress will be made on this instance.
// This func essentially terminates instance (no QBFT-related progress is done afterward), releasing all the resources
// it spawned.
// Both MarkDecided and MarkIrrelevant can be called on the same instance, these calls do not conflict.
func (i *Instance) MarkIrrelevant() {
	i.markedIrrelevant = true
	i.roundTimer.Stop()
}

func (i *Instance) Broadcast(msg *spectypes.SignedSSVMessage) error {
	if !i.IsRelevant() {
		return spectypes.NewError(spectypes.InstanceStoppedProcessingMessagesErrorCode, "instance is no longer considered relevant")
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

// ProcessMsg processes a new QBFT message.
// The returned bool/value pair reports whether this call newly decided the
// instance. Callers that need the post-call state should inspect State/IsDecided.
func (i *Instance) ProcessMsg(ctx context.Context, logger *zap.Logger, msg *specqbft.ProcessingMessage) (decided bool, decidedValue []byte, aggregatedCommit *spectypes.SignedSSVMessage, err error) {
	if !i.IsRelevant() {
		return false, nil, nil, spectypes.NewError(spectypes.InstanceStoppedProcessingMessagesErrorCode, "instance is no longer considered relevant")
	}

	if err := i.BaseMsgValidation(msg); err != nil {
		return false, nil, nil, fmt.Errorf("invalid signed message: %w", err)
	}

	res := i.processMsgF.Run(func() any {
		switch msg.QBFTMessage.MsgType {
		case specqbft.ProposalMsgType:
			return i.uponProposal(ctx, logger, msg)
		case specqbft.PrepareMsgType:
			return i.uponPrepare(ctx, logger, msg)
		case specqbft.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.uponCommit(ctx, logger, msg)
			if decided {
				err := i.MarkDecided(msg.QBFTMessage.Round, decidedValue)
				if err != nil {
					return fmt.Errorf("mark as decided: %w", err)
				}
			}
			return err
		case specqbft.RoundChangeMsgType:
			return i.uponRoundChange(ctx, logger, msg)
		default:
			return fmt.Errorf("signed message type not supported")
		}
	})
	if res != nil {
		return false, nil, nil, res.(error)
	}
	return decided, decidedValue, aggregatedCommit, nil
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
		return fmt.Errorf("signed message type not supported")
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

// bumpToRound pushes this instance to a higher round, also scheduling a timeout for it.
func (i *Instance) bumpToRound(round specqbft.Round) {
	if round > i.State.Round {
		i.State.ProposalAcceptedForCurrentRound = nil
		i.State.Round = round
		i.roundTimer.TimeoutForRound(round)
	}
}

// IsRelevant will return true if instance can process messages
func (i *Instance) IsRelevant() bool {
	return !i.markedIrrelevant && i.State.Round < i.config.GetCutOffRound()
}
