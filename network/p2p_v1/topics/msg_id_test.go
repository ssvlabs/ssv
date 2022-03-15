package topics

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSSVMsgID(t *testing.T) {
	t.Run("consensus msg", func(t *testing.T) {
		msgData := `{"message":{"type":3,"round":1,"identifier":"OTFiZGZjOWQxYzU4NzZkYTEwY...","height":28276,"value":"mB0aAAAAAAA4AAAAAAAAADpTC1djq..."},"signature":"jrB0+Z9zyzzVaUpDMTlCt6Om9mj...","signer_ids":[2,3,4]}`
		msg := protocol.SSVMessage{
			MsgType: protocol.SSVConsensusMsgType,
			ID:      []byte("OTFiZGZjOWQxYzU4NzZkYTEwY"),
			Data:    []byte(msgData),
		}
		raw, err := msg.MarshalJSON()
		require.NoError(t, err)
		mid := SSVMsgID(raw)
		require.Greater(t, len(mid), 0)
		require.Equal(t, "70068f6f9029f3ae5d217e6d", fmt.Sprintf("%x", mid))
	})

	t.Run("empty msg", func(t *testing.T) {
		require.Len(t, SSVMsgID([]byte{}), 0)
		require.Len(t, SSVMsgID(nil), 0)
	})
}
