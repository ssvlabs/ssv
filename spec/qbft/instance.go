package qbft

import (
	"encoding/json"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
	"sync"
)

type ProposedValueCheck func(data []byte) error

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *State
	config IConfig

	processMsgF *types.ThreadSafeF
	startOnce   sync.Once
	StartValue  []byte
}

func NewInstance(
	config IConfig,
	share *types.Share,
	identifier []byte,
	height Height,
) *Instance {
	return &Instance{
		State: &State{
			Share:                share,
			ID:                   identifier,
			Round:                FirstRound,
			Height:               height,
			LastPreparedRound:    NoRound,
			ProposeContainer:     NewMsgContainer(),
			PrepareContainer:     NewMsgContainer(),
			CommitContainer:      NewMsgContainer(),
			RoundChangeContainer: NewMsgContainer(),
		},
		config:      config,
		processMsgF: types.NewThreadSafeF(),
	}
}

// Start is an interface implementation
func (i *Instance) Start(value []byte, height Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.State.Round = FirstRound
		i.State.Height = height

		// propose if this node is the proposer
		if proposer(i.State, FirstRound) == i.State.Share.OperatorID {
			proposal, err := createProposal(i.State, i.config, i.StartValue, nil, nil)
			if err != nil {
				// TODO log
			}
			if err := i.config.GetNetwork().Broadcast(proposal); err != nil {
				// TODO - log
			}
		}
	})
}

// ProcessMsg processes a new QBFT msg, returns non nil error on msg processing error
func (i *Instance) ProcessMsg(msg *SignedMessage) (decided bool, decidedValue []byte, aggregatedCommit *SignedMessage, err error) {
	if err := msg.Validate(); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	res := i.processMsgF.Run(func() interface{} {
		switch msg.Message.MsgType {
		case ProposalMsgType:
			return uponProposal(i.State, i.config, msg, i.State.ProposeContainer)
		case PrepareMsgType:
			return uponPrepare(i.State, i.config, msg, i.State.PrepareContainer, i.State.CommitContainer)
		case CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = UponCommit(i.State, i.config, msg, i.State.CommitContainer)
			i.State.Decided = decided
			if decided {
				i.State.DecidedValue = decidedValue
			}

			// TODO - Roberto comment: we should send a Decided msg here
			return err
		case RoundChangeMsgType:
			return uponRoundChange(i.State, i.config, msg, i.State.RoundChangeContainer, i.config.GetValueCheck())
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

// GetHeight interface implementation
func (i *Instance) GetHeight() Height {
	return i.State.Height
}

// Encode implementation
func (i *Instance) Encode() ([]byte, error) {
	return json.Marshal(i)
}

// Decode implementation
func (i *Instance) Decode(data []byte) error {
	return json.Unmarshal(data, &i)
}
