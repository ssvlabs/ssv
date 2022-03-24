package v1

import (
	v1 "github.com/bloxapp/ssv/protocol/v1"
	"testing"
)

func TestAdapterV1_ConvertToV0(t *testing.T) {
	var messagesV0 []*v1.SSVMessage
	for i := 0; i < 4; i++ {
		messagesV0 = append(messagesV0, &v1.SSVMessage{
			MsgType: 0,
			ID:      nil,
			Data:    nil,
		})
	}

	//for i, msg := range messagesV0{
	//	msgV0, err := convertToV0Message(msg)
	//	require.NoError(t, err)
	//	//msgV0.Type
	//}

	t.Run("ibft message", func(t *testing.T) {

	})

	t.Run("decided message", func(t *testing.T) {

	})
}
