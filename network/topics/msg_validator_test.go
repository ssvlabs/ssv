package topics

import (
	"context"
	"testing"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/storage"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

func TestMsgValidator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := storage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: v1.ValidatorStateActiveOngoing,
			},
			Liquidated: false,
		},
	}
	require.NoError(t, ns.Shares().Save(nil, share))

	mv := validation.NewMessageValidator(networkconfig.TestNetwork, validation.WithNodeStorage(ns))
	require.NotNil(t, mv)

	slot := networkconfig.TestNetwork.Beacon.GetBeaconNetwork().EstimatedCurrentSlot()

	t.Run("valid consensus msg", func(t *testing.T) {
		msg, err := dummySSVConsensusMsg(share.ValidatorPubKey, qbft.Height(slot))
		require.NoError(t, err)

		raw, err := msg.Encode()
		require.NoError(t, err)

		topics := commons.ValidatorTopicID(share.ValidatorPubKey)
		pmsg := newPBMsg(raw, commons.GetTopicFullName(topics[0]), []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"))
		res := mv.ValidatePubsubMessage(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationAccept, res)
	})

	// TODO: enable once topic validation is in place
	//t.Run("wrong topic", func(t *testing.T) {
	//	pkHex := "b5de683dbcb3febe8320cc741948b9282d59b75a6970ed55d6f389da59f26325331b7ea0e71a2552373d0debb6048b8a"
	//	msg, err := dummySSVConsensusMsg(share.ValidatorPubKey, 15160)
	//	require.NoError(t, err)
	//	raw, err := msg.Encode()
	//	require.NoError(t, err)
	//	pk, err := hex.DecodeString("a297599ccf617c3b6118bbd248494d7072bb8c6c1cc342ea442a289415987d306bad34415f89469221450a2501a832ec")
	//	require.NoError(t, err)
	//	topics := commons.ValidatorTopicID(pk)
	//	pmsg := newPBMsg(raw, topics[0], []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"))
	//	res := mv.ValidateP2PMessage(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
	//	require.Equal(t, res, pubsub.ValidationReject)
	//})

	t.Run("empty message", func(t *testing.T) {
		pmsg := newPBMsg([]byte{}, "xxx", []byte{})
		res := mv.ValidatePubsubMessage(context.Background(), "xxxx", pmsg)
		require.Equal(t, pubsub.ValidationReject, res)
	})

	// TODO: enable once topic validation is in place
	//t.Run("invalid validator public key", func(t *testing.T) {
	//	msg, err := dummySSVConsensusMsg("10101011", 1)
	//	require.NoError(t, err)
	//	raw, err := msg.Encode()
	//	require.NoError(t, err)
	//	pmsg := newPBMsg(raw, "xxx", []byte{})
	//	res := mv.ValidateP2PMessage(context.Background(), "xxxx", pmsg)
	//	require.Equal(t, res, pubsub.ValidationReject)
	//})
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

func dummySSVConsensusMsg(pk spectypes.ValidatorPK, height qbft.Height) (*spectypes.SSVMessage, error) {
	id := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, pk, spectypes.BNRoleAttester)
	ks := spectestingutils.Testing4SharesSet()
	validSignedMessage := spectestingutils.TestingRoundChangeMessageWithHeightAndIdentifier(ks.Shares[1], 1, height, id[:])

	encodedSignedMessage, err := validSignedMessage.Encode()
	if err != nil {
		return nil, err
	}

	return &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    encodedSignedMessage,
	}, nil
}
