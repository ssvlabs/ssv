package v2

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSSVMsgID(t *testing.T) {
	t.Run("consensus msg", func(t *testing.T) {
		f := ForkV2{}
		msgData := `{"message":{"type":3,"round":1,"identifier":"OTFiZGZjOWQxYzU4NzZkYTEwY...","height":28276,"value":"mB0aAAAAAAA4AAAAAAAAADpTC1djq..."},"signature":"jrB0+Z9zyzzVaUpDMTlCt6Om9mj...","signer_ids":[2,3,4]}`
		msg := message.SSVMessage{
			MsgType: message.SSVConsensusMsgType,
			ID:      []byte("OTFiZGZjOWQxYzU4NzZkYTEwY"),
			Data:    []byte(msgData),
		}
		raw, err := msg.MarshalJSON()
		require.NoError(t, err)
		mid := f.MsgID()(raw)
		require.Greater(t, len(mid), 0)
		require.Equal(t, "70068f6f9029f3ae5d217e6d", fmt.Sprintf("%x", mid))
	})

	t.Run("empty msg", func(t *testing.T) {
		f := ForkV2{}
		require.Len(t, f.MsgID()([]byte{}), 0)
		require.Len(t, f.MsgID()(nil), 0)
	})
}
