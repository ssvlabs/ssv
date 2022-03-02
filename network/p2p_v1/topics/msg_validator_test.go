package topics

import (
	"context"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestMsgValidator(t *testing.T) {
	//logger := zap.L()
	logger := zaptest.NewLogger(t)
	f := forksv1.ForkV1{}
	mv := newMsgValidator(logger, &f)
	require.NotNil(t, mv)

	msgData := `{"message":{"type":3,"round":1,"identifier":"OTFiZGZjOWQxYzU4NzZkYTEwY...","height":28276,"value":"mB0aAAAAAAA4AAAAAAAAADpTC1djq..."},"signature":"jrB0+Z9zyzzVaUpDMTlCt6Om9mj...","signer_ids":[2,3,4]}`
	msg := protocol.SSVMessage{
		MsgType: protocol.SSVConsensusMsgType,
		MsgID:   []byte("OTFiZGZjOWQxYzU4NzZkYTEwY"),
		Data:    []byte(msgData),
	}
	raw, err := msg.Encode()
	require.NoError(t, err)

	t.Run("empty message", func(t *testing.T) {
		pmsg := newPBMsg([]byte{}, "xxx")
		res := mv(context.Background(), "xxxx", pmsg)
		require.Equal(t, res, pubsub.ValidationReject)
	})

	t.Run("invalid validator public key", func(t *testing.T) {
		pmsg := newPBMsg(raw, "xxx")
		res := mv(context.Background(), "xxxx", pmsg)
		require.Equal(t, res, pubsub.ValidationReject)
	})

}

func newPBMsg(data []byte, topic string) *pubsub.Message {
	pmsg := &pubsub.Message{
		Message: &ps_pb.Message{},
	}
	pmsg.Data = data
	pmsg.Topic = &topic
	return pmsg
}
