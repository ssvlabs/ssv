package topics

import (
	"context"
	"testing"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func TestMsgValidator(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, 1),
		Status:     v1.ValidatorStateActiveOngoing,
		Liquidated: false,
	}

	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
	require.NoError(t, err)

	require.NoError(t, ns.Shares().Save(nil, share))

	committeeID := share.CommitteeID()

	signatureVerifier := signatureverifier.NewSignatureVerifier(ns)
	mv := validation.New(
		networkconfig.TestNetwork,
		ns.ValidatorStore(),
		ns,
		dutystore.New(),
		signatureVerifier,
		phase0.Epoch(0),
		validation.WithLogger(logger),
	)

	require.NotNil(t, mv)

	slot := networkconfig.TestNetwork.Beacon.GetBeaconNetwork().EstimatedCurrentSlot()

	operatorID := uint64(1)
	operatorPrivateKey := ks.OperatorKeys[operatorID]

	operatorPubKey, err := rsaencryption.PublicKeyToBase64PEM(&operatorPrivateKey.PublicKey)
	require.NoError(t, err)

	od := &storage.OperatorData{
		PublicKey:    []byte(operatorPubKey),
		OwnerAddress: common.Address{},
		ID:           operatorID,
	}

	found, err := ns.SaveOperatorData(nil, od)
	require.False(t, found)
	require.NoError(t, err)

	operatorSigner := spectestingutils.NewOperatorSigner(ks, operatorID)

	t.Run("valid consensus msg", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg(committeeID[:], qbft.Height(slot))
		require.NoError(t, err)

		sig, err := operatorSigner.SignSSVMessage(ssvMsg)
		require.NoError(t, err)

		signedSSVMessage := &spectypes.SignedSSVMessage{
			Signatures:  [][]byte{sig},
			OperatorIDs: []spectypes.OperatorID{operatorID},
			SSVMessage:  ssvMsg,
		}

		encodedMsg, err := signedSSVMessage.Encode()
		require.NoError(t, err)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Topic: &topicID,
				Data:  encodedMsg,
			},
		}
		res := mv.Validate(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationAccept, res)
	})

	t.Run("wrong topic", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg(committeeID[:], qbft.Height(slot))
		require.NoError(t, err)

		sig, err := operatorSigner.SignSSVMessage(ssvMsg)
		require.NoError(t, err)

		signedSSVMessage := &spectypes.SignedSSVMessage{
			Signatures:  [][]byte{sig},
			OperatorIDs: []spectypes.OperatorID{operatorID},
			SSVMessage:  ssvMsg,
		}

		encodedMsg, err := signedSSVMessage.Encode()
		require.NoError(t, err)

		topicID := "wrong_topic_id"

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Topic: &topicID,
				Data:  encodedMsg,
			},
		}
		res := mv.Validate(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationIgnore, res)
	})

	t.Run("empty message", func(t *testing.T) {
		pmsg := newPBMsg([]byte{}, "xxx", []byte{})
		res := mv.Validate(context.Background(), "xxxx", pmsg)
		require.Equal(t, pubsub.ValidationReject, res)
	})

	t.Run("invalid validator public key", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg([]byte{1, 2, 3, 4, 5}, qbft.Height(slot))
		require.NoError(t, err)

		sig, err := operatorSigner.SignSSVMessage(ssvMsg)
		require.NoError(t, err)

		signedSSVMessage := &spectypes.SignedSSVMessage{
			Signatures:  [][]byte{sig},
			OperatorIDs: []spectypes.OperatorID{operatorID},
			SSVMessage:  ssvMsg,
		}

		encodedMsg, err := signedSSVMessage.Encode()
		require.NoError(t, err)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Topic: &topicID,
				Data:  encodedMsg,
			},
		}
		res := mv.Validate(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationIgnore, res)
	})
}

func newPBMsg(data []byte, topic string, from []byte) *pubsub.Message {
	pmsg := &pubsub.Message{
		Message: &pspb.Message{},
	}
	pmsg.Data = data
	pmsg.Topic = &topic
	pmsg.From = from
	return pmsg
}

func dummySSVConsensusMsg(dutyExecutorID []byte, height qbft.Height) (*spectypes.SSVMessage, error) {
	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, dutyExecutorID, spectypes.RoleCommittee)
	qbftMsg := &qbft.Message{
		MsgType:    qbft.RoundChangeMsgType,
		Height:     height,
		Round:      qbft.FirstRound,
		Identifier: id[:],
		Root:       spectestingutils.TestingQBFTRootData,
	}

	encodedQBFTMsg, err := qbftMsg.Encode()
	if err != nil {
		return nil, err
	}

	return &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    encodedQBFTMsg,
	}, nil
}
