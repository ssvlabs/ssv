package instance

import (
	"encoding/json"
	"sync"

	"github.com/ssvlabs/ssv/logging/fields"

	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *types.State
	config qbft.IConfig

	processMsgF *genesisspectypes.ThreadSafeF
	startOnce   sync.Once

	forceStop  bool
	StartValue []byte

	metrics *metrics
}

func NewInstance(
	config qbft.IConfig,
	share *spectypes.Share,
	identifier []byte,
	height genesisspecqbft.Height,
) *Instance {
	msgId := genesisspectypes.MessageIDFromBytes(identifier)
	return &Instance{
		State: &types.State{
			Share:                share,
			ID:                   identifier,
			Round:                genesisspecqbft.FirstRound,
			Height:               height,
			LastPreparedRound:    genesisspecqbft.NoRound,
			ProposeContainer:     genesisspecqbft.NewMsgContainer(),
			PrepareContainer:     genesisspecqbft.NewMsgContainer(),
			CommitContainer:      genesisspecqbft.NewMsgContainer(),
			RoundChangeContainer: genesisspecqbft.NewMsgContainer(),
		},
		config:      config,
		processMsgF: genesisspectypes.NewThreadSafeF(),
		metrics:     newMetrics(msgId),
	}
}

func (i *Instance) ForceStop() {
	i.forceStop = true
}

// Start is an interface implementation
func (i *Instance) Start(logger *zap.Logger, value []byte, height genesisspecqbft.Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.bumpToRound(genesisspecqbft.FirstRound)
		i.State.Height = height
		i.metrics.StartStage()

		i.config.GetTimer().TimeoutForRound(height, genesisspecqbft.FirstRound)

		logger = logger.With(
			fields.Round(specqbft.Round(i.State.Round)),
			fields.Height(specqbft.Height(i.State.Height)))

		proposerID := proposer(i.State, i.GetConfig(), genesisspecqbft.FirstRound)
		logger.Debug("ℹ️ starting QBFT instance", zap.Uint64("leader", proposerID))

		// propose if this node is the proposer
		if proposerID == i.config.GetOperatorID() {
			proposal, err := CreateProposal(i.State, i.config, i.StartValue, nil, nil)
			// nolint
			if err != nil {
				logger.Warn("❗ failed to create proposal", zap.Error(err))
				// TODO align spec to add else to avoid broadcast errored proposal
			} else {
				// nolint
				logger = logger.With(fields.Root(proposal.Message.Root))
				logger.Debug("📢 leader broadcasting proposal message")
				if err := i.Broadcast(logger, proposal); err != nil {
					logger.Warn("❌ failed to broadcast proposal", zap.Error(err))
				}
			}
		}
	})
}

func (i *Instance) Broadcast(logger *zap.Logger, msg *genesisspecqbft.SignedMessage) error {
	if !i.CanProcessMessages() {
		return errors.New("instance stopped processing messages")
	}
	byts, err := msg.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode message")
	}

	msgID := genesisspectypes.MessageID{}
	copy(msgID[:], msg.Message.Identifier)

	msgToBroadcast := &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.SSVConsensusMsgType,
		MsgID:   msgID,
		Data:    byts,
	}
	return i.config.GetNetwork().Broadcast(msgToBroadcast)
}

func allSigners(all []*genesisspecqbft.SignedMessage) []genesisspectypes.OperatorID {
	signers := make([]genesisspectypes.OperatorID, 0, len(all))
	for _, m := range all {
		signers = append(signers, m.Signers...)
	}
	return signers
}

// ProcessMsg processes a new QBFT msg, returns non nil error on msg processing error
func (i *Instance) ProcessMsg(logger *zap.Logger, msg *genesisspecqbft.SignedMessage) (decided bool, decidedValue []byte, aggregatedCommit *genesisspecqbft.SignedMessage, err error) {
	if !i.CanProcessMessages() {
		return false, nil, nil, errors.New("instance stopped processing messages")
	}

	if err := i.BaseMsgValidation(msg); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	res := i.processMsgF.Run(func() interface{} {
		switch msg.Message.MsgType {
		case genesisspecqbft.ProposalMsgType:
			return i.uponProposal(logger, msg, i.State.ProposeContainer)
		case genesisspecqbft.PrepareMsgType:
			return i.uponPrepare(logger, msg, i.State.PrepareContainer)
		case genesisspecqbft.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.UponCommit(logger, msg, i.State.CommitContainer)
			if decided {
				i.State.Decided = decided
				i.State.DecidedValue = decidedValue
			}
			return err
		case genesisspecqbft.RoundChangeMsgType:
			return i.uponRoundChange(logger, i.StartValue, msg, i.State.RoundChangeContainer, i.config.GetValueCheckF())
		default:
			return errors.New("signed message type not supported")
		}
	})
	if res != nil {
		return false, nil, nil, res.(error)
	}
	return i.State.Decided, i.State.DecidedValue, aggregatedCommit, nil
}

func (i *Instance) BaseMsgValidation(msg *genesisspecqbft.SignedMessage) error {
	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "invalid signed message")
	}

	if msg.Message.Round < i.State.Round {
		return errors.New("past round")
	}

	switch msg.Message.MsgType {
	case genesisspecqbft.ProposalMsgType:
		return isValidProposal(
			i.State,
			i.config,
			msg,
			i.config.GetValueCheckF(),
			i.State.Share.Committee,
		)
	case genesisspecqbft.PrepareMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}
		return validSignedPrepareForHeightRoundAndRoot(
			i.config,
			msg,
			i.State.Height,
			i.State.Round,
			proposedMsg.Message.Root,
			i.State.Share.Committee,
		)
	case genesisspecqbft.CommitMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}
		return validateCommit(
			i.config,
			msg,
			i.State.Height,
			i.State.Round,
			i.State.ProposalAcceptedForCurrentRound,
			i.State.Share.Committee,
		)
	case genesisspecqbft.RoundChangeMsgType:
		return validRoundChangeForData(i.State, i.config, msg, i.State.Height, msg.Message.Round, msg.FullData)
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
func (i *Instance) GetHeight() genesisspecqbft.Height {
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
func (i *Instance) bumpToRound(round genesisspecqbft.Round) {
	i.State.Round = round
	i.metrics.SetRound(round)
}

// CanProcessMessages will return true if instance can process messages
func (i *Instance) CanProcessMessages() bool {
	return !i.forceStop && int(i.State.Round) < CutoffRound
}
