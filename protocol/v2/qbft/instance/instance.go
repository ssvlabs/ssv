package instance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type operatorStore interface {
	OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error)
}

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	logger        *zap.Logger
	State         *specqbft.State
	config        qbft.IConfig
	signer        ssvtypes.OperatorSigner
	operatorStore operatorStore

	processMsgF *spectypes.ThreadSafeF
	startOnce   sync.Once

	forceStop  bool
	StartValue []byte

	metrics *metrics
}

func NewInstance(
	operatorStore operatorStore,
	config qbft.IConfig,
	committeeMember *spectypes.CommitteeMember,
	identifier []byte,
	height specqbft.Height,
	signer ssvtypes.OperatorSigner,
) *Instance {
	var name string
	if len(identifier) == 56 {
		name = spectypes.MessageID(identifier).GetRoleType().String()
	} else {
		name = base64.StdEncoding.EncodeToString(identifier)
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
		metrics:     newMetrics(name),
	}
}

func (i *Instance) ForceStop() {
	i.forceStop = true
}

// Start is an interface implementation
func (i *Instance) Start(ctx context.Context, logger *zap.Logger, value []byte, height specqbft.Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.logger = logger.Named("qbft_instance")
		i.bumpToRound(ctx, specqbft.FirstRound)
		i.State.Height = height
		i.metrics.StartStage()
		i.config.GetTimer().TimeoutForRound(height, specqbft.FirstRound)

		logger = logger.With(
			fields.Round(i.State.Round),
			fields.Height(i.State.Height))

		proposerID := proposer(i.State.CommitteeMember.Committee, i.GetConfig(), specqbft.FirstRound, i.State.Height)

		logger.Debug("ℹ️ starting QBFT instance", zap.Uint64("leader", proposerID))

		// propose if this node is the proposer
		if proposerID == i.State.CommitteeMember.OperatorID {
			proposal, err := CreateProposal(i.State, i.signer, i.StartValue, nil, nil)
			// nolint
			if err != nil {
				logger.Warn("❗ failed to create proposal", zap.Error(err))
				// TODO align spec to add else to avoid broadcast errored proposal
			} else {

				r, err := specqbft.HashDataRoot(i.StartValue) // @TODO (better than decoding?)
				if err != nil {
					logger.Warn("❗ failed to hash input data", zap.Error(err))
					return
				}
				// nolint
				logger = logger.With(fields.Root(r))
				logger.Debug("📢 leader broadcasting proposal message")
				if err := i.Broadcast(logger, proposal); err != nil {
					logger.Warn("❌ failed to broadcast proposal", zap.Error(err))
				}
			}
		}

		go i.RefreshState()
	})
}

func (i *Instance) Broadcast(logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	if !i.CanProcessMessages() {
		return errors.New("instance stopped processing messages")
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
		return false, nil, nil, errors.New("instance stopped processing messages")
	}

	if err := i.BaseMsgValidation(msg); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	res := i.processMsgF.Run(func() interface{} {

		switch msg.QBFTMessage.MsgType {
		case specqbft.ProposalMsgType:
			return i.uponProposal(ctx, logger, msg, i.State.ProposeContainer)
		case specqbft.PrepareMsgType:
			return i.uponPrepare(ctx, logger, msg, i.State.PrepareContainer)
		case specqbft.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.UponCommit(ctx, logger, msg, i.State.CommitContainer)
			if decided {
				i.State.Decided = decided
				i.State.DecidedValue = decidedValue
			}
			return err
		case specqbft.RoundChangeMsgType:
			return i.uponRoundChange(ctx, logger, i.StartValue, msg, i.State.RoundChangeContainer, i.config.GetValueCheckF())
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

	if msg.QBFTMessage.Round < i.State.Round {
		return errors.New("past round")
	}

	switch msg.QBFTMessage.MsgType {
	case specqbft.ProposalMsgType:
		return isValidProposal(
			i.State,
			i.config,
			msg,
			i.config.GetValueCheckF(),
		)
	case specqbft.PrepareMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}

		return validSignedPrepareForHeightRoundAndRootIgnoreSignature(
			msg,
			i.State.Height,
			i.State.Round,
			proposedMsg.QBFTMessage.Root,
			i.State.CommitteeMember.Committee,
		)
	case specqbft.CommitMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}
		return validateCommit(
			msg,
			i.State.Height,
			i.State.Round,
			i.State.ProposalAcceptedForCurrentRound,
			i.State.CommitteeMember.Committee,
		)
	case specqbft.RoundChangeMsgType:
		return validRoundChangeForDataIgnoreSignature(i.State, i.config, msg, i.State.Height, msg.QBFTMessage.Round, msg.SignedMessage.FullData)
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
func (i *Instance) bumpToRound(ctx context.Context, round specqbft.Round) {
	i.State.Round = round
}

// CanProcessMessages will return true if instance can process messages
func (i *Instance) CanProcessMessages() bool {
	return !i.forceStop && i.State.Round < i.config.GetCutOffRound()
}

func (i *Instance) RefreshState() {
	const interval = time.Second * 15
	i.logger.Info("launching QBFT instance state refresh", zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			activeOperators := make([]*spectypes.Operator, 0, len(i.State.CommitteeMember.Committee))
			for _, operator := range i.State.CommitteeMember.Committee {
				exist, err := i.operatorStore.OperatorsExist(nil, []spectypes.OperatorID{operator.OperatorID})
				if err != nil {
					i.logger.Error("could not fetch operator from the store", fields.OperatorID(operator.OperatorID))
					continue
				}
				if !exist {
					i.logger.Info("operator no longer exists in committee. Will update committee",
						fields.OperatorID(operator.OperatorID),
						fields.CommitteeID(i.State.CommitteeMember.CommitteeID))
				}

				i.logger.Info("operator is active, adding to the list", fields.OperatorID(operator.OperatorID))
				activeOperators = append(activeOperators, operator)
			}

			i.logger.Info("updating committee operator list",
				fields.CommitteeID(i.State.CommitteeMember.CommitteeID),
				zap.Int("active_operators", len(activeOperators)),
				zap.Int("previous_operators", len(i.State.CommitteeMember.Committee)),
			)

			i.State.CommitteeMember.Committee = activeOperators
		default:
			if i.forceStop {
				return
			}
		}
	}
}
