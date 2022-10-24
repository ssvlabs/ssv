package instance

import (
	"encoding/json"
	"fmt"
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/pkg/errors"
	"sync"
)

type ProposedValueCheckF func(data []byte) error
type ProposerF func(state *qbftspec.State, round qbftspec.Round) types.OperatorID

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *qbftspec.State
	config controller.IConfig

	processMsgF *types.ThreadSafeF
	startOnce   sync.Once
	StartValue  []byte
}

func NewInstance(
	config controller.IConfig,
	share *types.Share,
	identifier []byte,
	height qbftspec.Height,
) *Instance {
	return &Instance{
		State: &qbftspec.State{
			Share:                share,
			ID:                   identifier,
			Round:                qbftspec.FirstRound,
			Height:               height,
			LastPreparedRound:    qbftspec.NoRound,
			ProposeContainer:     qbftspec.NewMsgContainer(),
			PrepareContainer:     qbftspec.NewMsgContainer(),
			CommitContainer:      qbftspec.NewMsgContainer(),
			RoundChangeContainer: qbftspec.NewMsgContainer(),
		},
		config:      config,
		processMsgF: types.NewThreadSafeF(),
	}
}

// Start is an interface implementation
func (i *Instance) Start(value []byte, height qbftspec.Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.State.Round = qbftspec.FirstRound
		i.State.Height = height

		// propose if this node is the proposer
		if proposer(i.State, i.GetConfig(), qbftspec.FirstRound) == i.State.Share.OperatorID {
			proposal, err := CreateProposal(i.State, i.config, i.StartValue, nil, nil)
			// nolint
			if err != nil {
				fmt.Printf("%s\n", err.Error())
			}
			// nolint
			if err := i.Broadcast(proposal); err != nil {
				fmt.Printf("%s\n", err.Error())
			}
		}

		if err := i.config.GetNetwork().SyncHighestRoundChange(i.State.ID, i.State.Height); err != nil {
			fmt.Printf("%s\n", err.Error())
		}
	})
}

func (i *Instance) Broadcast(msg *qbftspec.SignedMessage) error {
	byts, err := msg.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode message")
	}

	msgID := types.MessageID{}
	copy(msgID[:], msg.Message.Identifier)

	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVConsensusMsgType,
		MsgID:   msgID,
		Data:    byts,
	}
	return i.config.GetNetwork().Broadcast(msgToBroadcast)
}

// ProcessMsg processes a new QBFT msg, returns non nil error on msg processing error
func (i *Instance) ProcessMsg(msg *qbftspec.SignedMessage) (decided bool, decidedValue []byte, aggregatedCommit *qbftspec.SignedMessage, err error) {
	if err := msg.Validate(); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	res := i.processMsgF.Run(func() interface{} {
		switch msg.Message.MsgType {
		case qbftspec.ProposalMsgType:
			return i.uponProposal(msg, i.State.ProposeContainer)
		case qbftspec.PrepareMsgType:
			return i.uponPrepare(msg, i.State.PrepareContainer, i.State.CommitContainer)
		case qbftspec.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.UponCommit(msg, i.State.CommitContainer)
			if decided {
				i.State.Decided = decided
				i.State.DecidedValue = decidedValue
			}
			return err
		case qbftspec.RoundChangeMsgType:
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

// IsDecided interface implementation
func (i *Instance) IsDecided() (bool, []byte) {
	return i.State.Decided, i.State.DecidedValue
}

// GetConfig returns the instance config
func (i *Instance) GetConfig() controller.IConfig {
	return i.config
}

// SetConfig returns the instance config
func (i *Instance) SetConfig(config controller.IConfig) {
	i.config = config
}

// GetHeight interface implementation
func (i *Instance) GetHeight() qbftspec.Height {
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
