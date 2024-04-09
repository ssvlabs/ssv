package validation

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/keys"
	"github.com/bloxapp/ssv/operator/storage"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"go.uber.org/zap/zaptest"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/network/commons"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
)

func BenchmarkVerifyRSASignature(b *testing.B) {
	logger := zaptest.NewLogger(b)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(b, err)

	ns, err := storage.NewNodeStorage(logger, db)
	require.NoError(b, err)

	const validatorIndex = 123
	const operatorID = spectypes.OperatorID(1)

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  validatorIndex,
			},
			Liquidated: false,
		},
	}
	err = ns.Shares().Save(nil, share)
	require.NoError(b, err)

	netCfg := networkconfig.TestNetwork

	roleAttester := spectypes.BNRoleAttester

	slot := netCfg.Beacon.FirstSlotAtEpoch(123456789)

	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))

	encoded, err := validSignedMessage.Encode()
	require.NoError(b, err)

	message := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
		Data:    encoded,
	}

	encodedMsg, err := commons.EncodeNetworkMsg(message)
	require.NoError(b, err)

	privKey, err := keys.GeneratePrivateKey()
	require.NoError(b, err)

	pubKey, err := privKey.Public().Base64()
	require.NoError(b, err)

	od := &registrystorage.OperatorData{
		ID:           operatorID,
		PublicKey:    pubKey,
		OwnerAddress: common.Address{},
	}

	found, err := ns.SaveOperatorData(nil, od)
	require.NoError(b, err)
	require.False(b, found)

	signature, err := privKey.Sign(encodedMsg)
	require.NoError(b, err)

	encodedMsg = commons.EncodeSignedSSVMessage(encodedMsg, operatorID, signature)

	topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
	pMsg := &pubsub.Message{
		Message: &pspb.Message{
			Topic: &topicID[0],
			Data:  encodedMsg,
		},
	}

	mv := NewMessageValidator(netCfg, WithNodeStorage(ns)).(*messageValidator)

	messageData := pMsg.GetData()
	decMessageData, operatorIDX, signature, err := commons.DecodeSignedSSVMessage(messageData)
	require.NoError(b, err)

	messageData = decMessageData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := mv.verifySignature(messageData, operatorIDX, signature)
		require.NoError(b, err)
	}
}
