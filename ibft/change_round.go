package ibft

import (
	"encoding/binary"
	"encoding/json"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) roundChangeInputValue() ([]byte, error) {
	preparedRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(preparedRound, i.state.PreparedRound)

	data := map[string][]byte{
		"prepared_round": preparedRound,
		"prepared_value": i.state.PreparedValue,
	}

	return json.Marshal(data)
}

func (i *iBFTInstance) uponChangeRoundTrigger() {
	i.log.Info("round timeout, changing round", zap.Uint64("round", i.state.Round))

	// bump round
	i.state.Round++

	// set time for next round change
	i.triggerRoundChangeOnTimer()

	// broadcast round change
	data, err := i.roundChangeInputValue()
	if err != nil {
		i.log.Error("failed to create round change data for round", zap.Uint64("round", i.state.Round), zap.Error(err))
	}
	broadcastMsg := &types.Message{
		Type:       types.RoundState_RoundChange,
		Round:      i.state.Round,
		Lambda:     i.state.Lambda,
		InputValue: data,
		IbftId:     i.state.IBFTId,
	}
	if err := i.network.Broadcast(broadcastMsg); err != nil {
		i.log.Error("could not broadcast round change message", zap.Error(err))
	}
}

func (i *iBFTInstance) uponChangeRoundMessage(msg *types.Message) {
	i.log.Info("changing round")
}
