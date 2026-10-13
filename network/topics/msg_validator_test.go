package topics

import (
	"math"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"

	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestMsgValidator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	beaconCfg := *networkconfig.TestNetwork.Beacon
	// Pin slot 0 to start right now so that by the time `mv.Validate` runs the message is
	// still comfortably inside round 1 for the committee role. Committee round 1 is accepted
	// as long as timeIntoSlot stays below ~(slotDuration/3 + 3*QuickTimeout) ≈ 10s — any
	// larger offset here would eat into that budget. A zero offset maximizes it.
	beaconCfg.GenesisTime = time.Now()
	// Pin the fork explicitly pre-Boole: the fixtures below build Alan topics/domains, so the
	// suite must stay deterministic under the SSV_TEST_BOOLE_FORK=post CI matrix, which flips
	// the global TestNetwork default.
	preBooleSSV := *networkconfig.TestNetwork.SSV
	preBooleSSV.Forks = networkconfig.SSVForks{Boole: phase0.Epoch(math.MaxUint64)}
	testNet := &networkconfig.Network{
		Beacon: &beaconCfg,
		SSV:    &preBooleSSV,
	}

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, 1),
		Status:     v1.ValidatorStateActiveOngoing,
		Liquidated: false,
	}

	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := operatorstorage.NewNodeStorage(testNet.Beacon, logger, db)
	require.NoError(t, err)

	require.NoError(t, ns.Shares().Save(nil, share))

	committeeID := share.CommitteeID()

	signatureVerifier := signatureverifier.NewSignatureVerifier(ns)
	mv := validation.New(
		testNet,
		ns.ValidatorStore(),
		ns,
		dutystore.New(),
		signatureVerifier,
		validation.WithLogger(logger),
	)

	require.NotNil(t, mv)

	slot := testNet.EstimatedCurrentSlot()

	operatorID := uint64(1)
	operatorPrivateKey := ks.OperatorKeys[operatorID]

	operatorPubKey, err := rsaencryption.PublicKeyToBase64PEM(&operatorPrivateKey.PublicKey)
	require.NoError(t, err)

	od := &storage.OperatorData{
		PublicKey:    operatorPubKey,
		OwnerAddress: common.Address{},
		ID:           operatorID,
	}

	found, err := ns.SaveOperatorData(nil, od)
	require.False(t, found)
	require.NoError(t, err)

	operatorSigner := spectestingutils.NewOperatorSigner(ks, operatorID)

	t.Run("valid consensus msg", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg(testNet.DomainType, committeeID[:], specqbft.Height(slot))
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

		topicID := commons.GetTopicFullName(commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0])

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Topic: &topicID,
				Data:  encodedMsg,
			},
		}
		res := mv.Validate(t.Context(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationAccept, res)
	})

	t.Run("wrong topic", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg(testNet.DomainType, committeeID[:], specqbft.Height(slot))
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
		res := mv.Validate(t.Context(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationIgnore, res)
	})

	t.Run("empty message", func(t *testing.T) {
		pmsg := newPBMsg([]byte{}, "xxx", []byte{})
		res := mv.Validate(t.Context(), "xxxx", pmsg)
		require.Equal(t, pubsub.ValidationReject, res)
	})

	t.Run("invalid validator public key", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg(testNet.DomainType, []byte{1, 2, 3, 4, 5}, specqbft.Height(slot))
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

		topicID := commons.GetTopicFullName(commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0])

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Topic: &topicID,
				Data:  encodedMsg,
			},
		}
		res := mv.Validate(t.Context(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
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

func dummySSVConsensusMsg(domainType spectypes.DomainType, dutyExecutorID []byte, height specqbft.Height) (*spectypes.SSVMessage, error) {
	id := ssvtestingutils.NewMsgID(domainType, dutyExecutorID, spectypes.RoleCommittee)
	qbftMsg := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     height,
		Round:      specqbft.FirstRound,
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
