package genesis

import (
	"fmt"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/stretchr/testify/require"
)

func TestSSVMsgID(t *testing.T) {
	t.Run("consensus msg", func(t *testing.T) {
		f := ForkGenesis{}
		msgData := `{"message":{"type":3,"round":1,"identifier":"OTFiZGZjOWQxYzU4NzZkYTEwY...","height":28276,"value":"mB0aAAAAAAA4AAAAAAAAADpTC1djq..."},"signature":"sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv","signer_ids":[2,3,4]}`
		msg := spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   specqbft.ControllerIdToMessageID([]byte("OTFiZGZjOWQxYzU4NzZkYTEwY")),
			Data:    []byte(msgData),
		}
		raw, err := msg.Encode()
		require.NoError(t, err)
		mid := f.MsgID()(raw)
		require.Greater(t, len(mid), 0)
		require.Equal(t, "fc0e576e6f9b3f6d00000000", fmt.Sprintf("%x", mid))
	})

	t.Run("empty msg", func(t *testing.T) {
		f := ForkGenesis{}
		require.Len(t, f.MsgID()([]byte{}), 0)
		require.Len(t, f.MsgID()(nil), 0)
	})
}
