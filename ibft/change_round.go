package ibft

import (
	"encoding/binary"
	"encoding/json"

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
	i.log.Infof("round %d timeout, changing round", i.state.Round)

	// bump round
	i.state.Round++

	// set time for next round change
	i.triggerRoundChangeOnTimer()

	// broadcast round change
	data, err := i.roundChangeInputValue()
	if err != nil {
		i.log.WithError(err).Errorf("failed to create round change data for round %d", i.state.Round)
	}
	broadcastMsg := &types.Message{
		Type:       types.MsgType_RoundChange,
		Round:      i.state.Round,
		Lambda:     i.state.Lambda,
		InputValue: data,
		IbftId:     i.state.IBFTId,
	}
	if err := i.network.Broadcast(broadcastMsg); err != nil {
		i.log.WithError(err).Errorf("could not broadcast round change message")
	}
}

func (i *iBFTInstance) uponChangeRoundMessage(msg *types.Message) {
	i.log.Infof("changing round")
}
