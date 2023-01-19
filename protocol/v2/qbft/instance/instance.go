package instance

import (
	"encoding/hex"
	"encoding/json"
	"sync"

	logging "github.com/ipfs/go-log"
	"go.uber.org/zap"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/pkg/errors"
)

var logger = logging.Logger("ssv/protocol/qbft/instance").Desugar()

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *specqbft.State
	config qbft.IConfig

	processMsgF *spectypes.ThreadSafeF
	startOnce   sync.Once
	StartValue  []byte

	logger *zap.Logger
}

func NewInstance(
	config qbft.IConfig,
	share *spectypes.Share,
	identifier []byte,
	height specqbft.Height,
) *Instance {
	msgId := spectypes.MessageIDFromBytes(identifier)
	return &Instance{
		State: &specqbft.State{
			Share:                share,
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
		processMsgF: spectypes.NewThreadSafeF(),
		logger: logger.With(zap.String("publicKey", hex.EncodeToString(msgId.GetPubKey())), zap.String("role", msgId.GetRoleType().String()),
			zap.Uint64("height", uint64(height))),
	}
}

// Start is an interface implementation
func (i *Instance) Start(value []byte, height specqbft.Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.State.Round = specqbft.FirstRound
		i.State.Height = height

		i.config.GetTimer().TimeoutForRound(specqbft.FirstRound)

		i.logger.Debug("starting QBFT instance")

		// propose if this node is the proposer
		if proposer(i.State, i.GetConfig(), specqbft.FirstRound) == i.State.Share.OperatorID {
			proposal, err := CreateProposal(i.State, i.config, i.StartValue, nil, nil)
			// nolint
			if err != nil {
				i.logger.Warn("failed to create proposal", zap.Error(err))
				// TODO align spec to add else to avoid broadcast errored proposal
			} else {
				// nolint
				if err := i.Broadcast(proposal); err != nil {
					i.logger.Warn("failed to broadcast proposal", zap.Error(err))
				}
			}
		}
	})
}

func (i *Instance) Broadcast(msg *specqbft.SignedMessage) error {
	byts, err := msg.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode message")
	}

	msgID := spectypes.MessageID{}
	copy(msgID[:], msg.Message.Identifier)

	msgToBroadcast := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   msgID,
		Data:    byts,
	}
	return i.config.GetNetwork().Broadcast(msgToBroadcast)
}

// ProcessMsg processes a new QBFT msg, returns non nil error on msg processing error
func (i *Instance) ProcessMsg(msg *specqbft.SignedMessage) (decided bool, decidedValue []byte, aggregatedCommit *specqbft.SignedMessage, err error) {
	if err := i.BaseMsgValidation(msg); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	res := i.processMsgF.Run(func() interface{} {
		switch msg.Message.MsgType {
		case specqbft.ProposalMsgType:
			return i.uponProposal(msg, i.State.ProposeContainer)
		case specqbft.PrepareMsgType:
			return i.uponPrepare(msg, i.State.PrepareContainer, i.State.CommitContainer)
		case specqbft.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.UponCommit(msg, i.State.CommitContainer)
			if decided {
				i.State.Decided = decided
				i.State.DecidedValue = decidedValue
			}
			return err
		case specqbft.RoundChangeMsgType:
			return i.uponRoundChange(i.StartValue, msg, i.State.RoundChangeContainer, i.config.GetValueCheckF())
		default:
			return errors.New("signed message type not supported")
		}
	})
	if res != nil {
		return false, nil, nil, res.(error)
	}
	return i.State.Decided, i.State.DecidedValue, aggregatedCommit, nil
}

func (i *Instance) BaseMsgValidation(msg *specqbft.SignedMessage) error {
	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "invalid signed message")
	}

	if msg.Message.Round < i.State.Round {
		return errors.New("past round")
	}

	switch msg.Message.MsgType {
	case specqbft.ProposalMsgType:
		return isValidProposal(
			i.State,
			i.config,
			msg,
			i.config.GetValueCheckF(),
			i.State.Share.Committee,
		)
	case specqbft.PrepareMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}
		acceptedProposalData, err := proposedMsg.Message.GetCommitData()
		if err != nil {
			return errors.Wrap(err, "could not get accepted proposal data")
		}
		return validSignedPrepareForHeightRoundAndValue(
			i.config,
			msg,
			i.State.Height,
			i.State.Round,
			acceptedProposalData.Data,
			i.State.Share.Committee,
		)
	case specqbft.CommitMsgType:
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
	case specqbft.RoundChangeMsgType:
		return validRoundChange(i.State, i.config, msg, i.State.Height, msg.Message.Round)
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
func (i *Instance) GetRoot() ([]byte, error) {
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
