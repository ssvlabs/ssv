package benchmark

import (
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"fmt"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/automation/commons"
	commons2 "github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/bloxapp/ssv/network/topics"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	testing2 "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

func BenchmarkValidation(t *testing.B) {
	t.StopTimer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	//logger := zap.L()
	logger := zaptest.NewLogger(t)
	f := genesis.ForkGenesis{}
	self := peer.ID("16Uiu2HAmNNPRh9pV2MXASMB7oAGCqdmFrYyp5tzutFiF2LN1xFCE")

	share, _, msgs, err := setup(4, 1000)
	require.NoError(t, err)

	var pmsgs []*pubsub.Message

	sk, err := commons2.GenNetworkKey()
	require.NoError(t, err)
	isk, err := commons2.ConvertToInterfacePrivkey(sk)
	require.NoError(t, err)
	pid, err := peer.IDFromPrivateKey(isk)
	require.NoError(t, err)

	pk := share.PublicKey.Serialize()
	valTopics := f.ValidatorTopicID(pk)
	topicName := f.GetTopicFullName(valTopics[0])

	for _, smsg := range msgs {
		data, err := smsg.Encode()
		require.NoError(t, err)
		id := spectypes.NewMsgID(share.PublicKey.Serialize(), spectypes.BNRoleAttester)
		msg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   id,
			Data:    data,
		}
		raw, err := msg.Encode()
		require.NoError(t, err)
		pmsg := newPBMsg(raw, topicName, []byte(pid.String()))
		require.NoError(t, signMessage(pid, isk, pmsg.Message))
		pmsgs = append(pmsgs, pmsg)
	}

	t.ResetTimer()
	t.StartTimer()

	t.Run("pubsub router signing", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pmsg := pmsgs[i%len(pmsgs)]
			ppmsg := newPBMsg(pmsg.GetData(), topicName, []byte(pid.String()))
			_ = signMessage(pid, isk, ppmsg.Message)
		}
	})

	t.Run("pubsub router verification", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pmsg := pmsgs[i%len(pmsgs)]
			_ = verifyMessageSignature(pmsg.Message)
		}
	})

	t.Run("topic msg validator", func(b *testing.B) {
		mv := topics.NewSSVMsgValidator(logger, &f, self)
		require.NotNil(t, mv)
		pi := peer.ID("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r")
		for i := 0; i < b.N; i++ {
			pmsg := pmsgs[i%len(pmsgs)]
			require.Equal(t, mv(ctx, pi, pmsg), pubsub.ValidationAccept)
		}
	})

	t.Run("topic msg validator with pipelines", func(b *testing.B) {
		ctrlFork := forksfactory.NewFork(forksprotocol.GenesisForkVersion)
		pip := ctrlFork.ValidateDecidedMsg(share)
		mv := topics.NewSSVMsgValidator(logger, &f, self, func(msg *spectypes.SSVMessage) error {
			sm := &specqbft.SignedMessage{}
			err := sm.Decode(msg.GetData())
			if err != nil {
				return err
			}
			return pip.Run(sm)
		})
		require.NotNil(t, mv)
		pi := peer.ID("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r")
		for i := 0; i < b.N; i++ {
			pmsg := pmsgs[i%len(pmsgs)]
			require.Equal(t, mv(ctx, pi, pmsg), pubsub.ValidationAccept)
		}
	})

	t.Run("topic msg validator with bls", func(b *testing.B) {
		pip := pipelines.Combine(
			signedmsg.AuthorizeMsg(share),
			signedmsg.ValidateQuorum(share.ThresholdSize()))
		mv := topics.NewSSVMsgValidator(logger, &f, self, func(msg *spectypes.SSVMessage) error {
			sm := &specqbft.SignedMessage{}
			err := sm.Decode(msg.GetData())
			if err != nil {
				return err
			}
			return pip.Run(sm)
		})
		require.NotNil(t, mv)
		pi := peer.ID("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r")
		for i := 0; i < b.N; i++ {
			pmsg := pmsgs[i%len(pmsgs)]
			require.Equal(t, mv(ctx, pi, pmsg), pubsub.ValidationAccept)
		}
	})
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

func setup(totalNodes, totalMsgs int) (*beacon.Share, map[spectypes.OperatorID]*bls.SecretKey, []*specqbft.SignedMessage, error) {
	oidsRaw := make([][]byte, 0)

	for i := 0; i < totalNodes; i++ {
		opKey, err := rsa.GenerateKey(crand.Reader, 2048)
		if err != nil {
			return nil, nil, nil, err
		}
		pub, err := rsaencryption.ExtractPublicKey(opKey)
		if err != nil {
			return nil, nil, nil, err
		}
		oidsRaw = append(oidsRaw, []byte(pub))
	}
	share, sks, err := commons.CreateShare(oidsRaw)
	if err != nil {
		return nil, nil, nil, err
	}
	oids := make([]spectypes.OperatorID, 0)
	keys := make(map[spectypes.OperatorID]*bls.SecretKey)
	for oid := range share.Committee {
		keys[oid] = sks[uint64(oid)]
		oids = append(oids, oid)
	}
	msgs, err := testing2.CreateMultipleSignedMessages(keys, specqbft.Height(0), specqbft.Height(totalMsgs), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
		commitData := specqbft.CommitData{Data: []byte(fmt.Sprintf("msg-data-%d", height))}
		commitDataBytes, err := commitData.Encode()
		if err != nil {
			panic(err)
		}

		id := spectypes.NewMsgID(share.PublicKey.Serialize(), spectypes.BNRoleAttester)
		return oids, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: id[:],
			Data:       commitDataBytes,
		}
	})
	return share, keys, msgs, err
}
