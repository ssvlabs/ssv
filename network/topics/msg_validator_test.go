package topics

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/utils/threshold"
)

func TestMsgValidator(t *testing.T) {
	pks := createSharePublicKeys(4)
	// TODO: use a validator, adjust the message for it
	mv := NewSSVMsgValidator(zaptest.NewLogger(t), nopMetrics{}, nil)
	require.NotNil(t, mv)

	t.Run("valid consensus msg", func(t *testing.T) {
		pkHex := pks[0]
		msg, err := dummySSVConsensusMsg(pkHex, 15160)
		require.NoError(t, err)
		raw, err := msg.Encode()
		require.NoError(t, err)
		pk, err := hex.DecodeString(pkHex)
		require.NoError(t, err)
		topics := commons.ValidatorTopicID(pk)
		pmsg := newPBMsg(raw, commons.GetTopicFullName(topics[0]), []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"))
		res := mv(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, res, pubsub.ValidationAccept)
	})

	// TODO: enable once topic validation is in place
	// t.Run("wrong topic", func(t *testing.T) {
	//	pkHex := "b5de683dbcb3febe8320cc741948b9282d59b75a6970ed55d6f389da59f26325331b7ea0e71a2552373d0debb6048b8a"
	//	msg, err := dummySSVConsensusMsg(pkHex, 15160)
	//	require.NoError(t, err)
	//	raw, err := msg.Encode()
	//	require.NoError(t, err)
	//	pk, err := hex.DecodeString("a297599ccf617c3b6118bbd248494d7072bb8c6c1cc342ea442a289415987d306bad34415f89469221450a2501a832ec")
	//	require.NoError(t, err)
	//	topics := commons.ValidatorTopicID(pk)
	//	pmsg := newPBMsg(raw, topics[0], []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"))
	//	res := mv(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
	//	require.Equal(t, res, pubsub.ValidationReject)
	// })

	t.Run("empty message", func(t *testing.T) {
		pmsg := newPBMsg([]byte{}, "xxx", []byte{})
		res := mv(context.Background(), "xxxx", pmsg)
		require.Equal(t, res, pubsub.ValidationReject)
	})

	// TODO: enable once topic validation is in place
	// t.Run("invalid validator public key", func(t *testing.T) {
	//	msg, err := dummySSVConsensusMsg("10101011", 1)
	//	require.NoError(t, err)
	//	raw, err := msg.Encode()
	//	require.NoError(t, err)
	//	pmsg := newPBMsg(raw, "xxx", []byte{})
	//	res := mv(context.Background(), "xxxx", pmsg)
	//	require.Equal(t, res, pubsub.ValidationReject)
	// })

}

func createSharePublicKeys(n int) []string {
	threshold.Init()

	var res []string
	for i := 0; i < n; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		pk := sk.GetPublicKey().SerializeToHexStr()
		res = append(res, pk)
	}
	return res
}

func newPBMsg(data []byte, topic string, from []byte) *pubsub.Message {
	pmsg := &pubsub.Message{
		Message: &ps_pb.Message{},
	}
	pmsg.Data = data
	pmsg.Topic = &topic
	pmsg.From = from
	return pmsg
}

func dummySSVConsensusMsg(pkHex string, height int) (*spectypes.SSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}
	id := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, pk, spectypes.BNRoleAttester)
	msgData := fmt.Sprintf(`{
	  "message": {
		"type": 3,
		"round": 2,
		"identifier": "%s",
		"height": %d,
		"value": "bk0iAAAAAAACAAAAAAAAAAbYXFSt2H7SQd5q5u+N0bp6PbbPTQjU25H1QnkbzTECahIBAAAAAADmi+NJfvXZ3iXp2cfs0vYVW+EgGD7DTTvr5EkLtiWq8WsSAQAAAAAAIC8dZTEdD3EvE38B9kDVWkSLy40j0T+TtSrrrBqVjo4="
	  },
	  "signature": "sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN",
	  "signer_ids": [1,3,4]
	}`, id, height)
	return &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    []byte(msgData),
	}, nil
}
