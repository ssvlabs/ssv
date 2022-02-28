package topics

import (
	"context"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestMsgValidator(t *testing.T) {
	mv := newMsgValidator(zap.L(), forksv1.New())
	require.NotNil(t, mv)

	msg := &pubsub.Message{
		Message:       &ps_pb.Message{},
		ReceivedFrom:  "xxxx",
		ValidatorData: nil,
	}
	res := mv(context.Background(), "xxxx", msg)
	require.Equal(t, res, pubsub.ValidationReject)
}
