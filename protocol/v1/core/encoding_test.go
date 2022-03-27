package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSSVMessage_JSONEncoding(t *testing.T) {
	pkHex := "b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400"
	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	require.Greater(t, len(pk), 0)
	id := NewIdentifier(pk, beacon.RoleTypeAttester)
	msgData := fmt.Sprintf(`{
	  "message": {
		"type": 3,
		"round": 2,
		"identifier": "%s",
		"height": 1,
		"value": "bk0iAAAAAAACAAAAA"
	  },
	  "signature": "sVV0fsvqQlqliKvN",
	  "signers": [1,3,4]
	}`, id)
	msg := SSVMessage{
		MsgType: SSVConsensusMsgType,
		ID:      id,
		Data:    []byte(msgData),
	}

	encoded, err := json.Marshal(&msg)
	require.NoError(t, err)

	decoded := SSVMessage{}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.True(t, bytes.Equal(msg.GetID(), decoded.GetID()))
}
