package instance

import (
	"encoding/json"
	"fmt"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"sync"

	"github.com/bloxapp/ssv/protocol/v2/types"
)

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *specqbft.State
	config types.IConfig

	processMsgF *spectypes.ThreadSafeF
	startOnce   sync.Once
	StartValue  []byte
}

func NewInstance(
	config types.IConfig,
	share *spectypes.Share,
	identifier []byte,
	height specqbft.Height,
) *Instance {
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
	}
}

// Start is an interface implementation
func (i *Instance) Start(value []byte, height specqbft.Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.State.Round = specqbft.FirstRound
		i.State.Height = height

		i.config.GetTimer().TimeoutForRound(specqbft.FirstRound)

		// propose if this node is the proposer
		if proposer(i.State, i.GetConfig(), specqbft.FirstRound) == i.State.Share.OperatorID {
			proposal, err := CreateProposal(i.State, i.config, i.StartValue, nil, nil)
			// nolint
			if err != nil {
				fmt.Printf("%s\n", err.Error())
				// TODO align spec to add else to avoid broadcast errored proposal
			} else {
				// nolint
				if err := i.Broadcast(proposal); err != nil {
					fmt.Printf("%s\n", err.Error())
				}
			}
		}
    
    // TODO will be removed upon https://github.com/bloxapp/SIPs/blob/main/sips/constant_qbft_timeout.md
		mid := spectypes.MessageIDFromBytes(i.State.ID)
		if err := i.config.GetNetwork().SyncHighestRoundChange(mid, i.State.Height); err != nil {
			fmt.Printf("%s\n", err.Error())
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
	if err := msg.Validate(); err != nil {
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

// IsDecided interface implementation
func (i *Instance) IsDecided() (bool, []byte) {
	if state := i.State; state != nil {
		return state.Decided, state.DecidedValue
	}
	return false, nil
}

// GetConfig returns the instance config
func (i *Instance) GetConfig() types.IConfig {
	return i.config
}

// SetConfig returns the instance config
func (i *Instance) SetConfig(config types.IConfig) {
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
