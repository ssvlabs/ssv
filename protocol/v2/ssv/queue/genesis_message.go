package queue

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

// TODO: consider moving to another package

type WrappedGenesisMessage struct {
	*genesisspectypes.SSVMessage
}

func (w WrappedGenesisMessage) GetID() spectypes.MessageID {
	return spectypes.MessageID(w.SSVMessage.GetID())
}

func (w WrappedGenesisMessage) GetType() spectypes.MsgType {
	return spectypes.MsgType(w.SSVMessage.GetType())
}
