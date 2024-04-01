package validation

import (
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/storage"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
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

	mv := NewMessageValidator(netCfg, WithNodeStorage(ns)).(*messageValidator)

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

	hash := sha256.Sum256(encodedMsg)
	privateKey, err := rsa.GenerateKey(crand.Reader, 2048)
	require.NoError(b, err)
	pubkey := privateKey.Public().(*rsa.PublicKey)

	pubKey, err := rsaencryption.ExtractPublicKey(pubkey)
	require.NoError(b, err)

	od := &registrystorage.OperatorData{
		ID:           operatorID,
		PublicKey:    []byte(pubKey),
		OwnerAddress: common.Address{},
	}

	found, err := ns.SaveOperatorData(nil, od)
	require.NoError(b, err)

	require.False(b, found)

	signature, err := rsa.SignPKCS1v15(crand.Reader, privateKey, crypto.SHA256, hash[:])
	require.NoError(b, err)

	encodedMsg = commons.EncodeSignedSSVMessage(encodedMsg, operatorID, signature)

	topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
	pMsg := &pubsub.Message{
		Message: &pspb.Message{
			Topic: &topicID[0],
			Data:  encodedMsg,
		},
	}

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
