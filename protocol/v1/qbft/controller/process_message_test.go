package controller

import (
	"github.com/bloxapp/ssv/ibft/proto"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	//"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestProcessLateCommitMsg(t *testing.T) {
	sks, _ := GenerateNodes(4)
	db := qbftstorage.NewQBFTStore(newInMemDb(), zap.L(), "attestation")

	share := beacon.Share{}
	share.PublicKey = sks[1].GetPublicKey()
	share.Committee = make(map[message.OperatorID]*beacon.Node, 4)
	identifier := format.IdentifierFormat(share.PublicKey.Serialize(), message.RoleTypeAttester.String()) // TODO should using fork to get identifier?

	ctrl := Controller{
		ValidatorShare: &beacon.Share{
			NodeID:    1,
			PublicKey: sks[1].GetPublicKey(),
			Committee: nil,
		},
	}
	ctrl.DecidedFactory = factory.NewDecidedFactory(logex.GetLogger(), strategy.ModeFullNode, db, nil)
	ctrl.DecidedStrategy = ctrl.DecidedFactory.GetStrategy()

	var sigs []*message.SignedMessage
	commitData, err := (&message.CommitData{Data: []byte("value")}).Encode()
	require.NoError(t, err)

	for i := 1; i < 4; i++ {
		sigs = append(sigs, SignMsg(t, uint64(i), sks[message.OperatorID(i)], &message.ConsensusMessage{
			Height:     2,
			MsgType:    message.CommitMsgType,
			Round:      3,
			Identifier: []byte(identifier),
			Data:       commitData,
		}, forksprotocol.GenesisForkVersion.String()))
	}
	decided, err := AggregateMessages(sigs)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expectedErr string
		updated     interface{}
		msg         *message.SignedMessage
	}{
		{
			"valid",
			"",
			struct{}{},
			SignMsg(t, 4, sks[4], &message.ConsensusMessage{
				Height:     message.Height(2),
				MsgType:    message.CommitMsgType,
				Round:      3,
				Identifier: []byte(identifier),
				Data:       commitData,
			}, forksprotocol.GenesisForkVersion.String()),
		},
		{
			"invalid",
			"could not aggregate commit message",
			nil,
			func() *message.SignedMessage {
				msg := SignMsg(t, 4, sks[4], &message.ConsensusMessage{
					Height:     2,
					MsgType:    message.CommitMsgType,
					Round:      3,
					Identifier: []byte(identifier),
					Data:       commitData,
				}, forksprotocol.GenesisForkVersion.String())
				msg.Signature = []byte("dummy")
				return msg
			}(),
		},
		{
			"not found",
			"",
			nil,
			SignMsg(t, 4, sks[4], &message.ConsensusMessage{
				Height:     message.Height(2),
				MsgType:    message.CommitMsgType,
				Round:      3,
				Identifier: []byte("xxx_ATTESTER"),
				Data:       commitData,
			}, forksprotocol.GenesisForkVersion.String()),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.SaveDecided(decided))
			updated, err := ctrl.ProcessLateCommitMsg(logex.GetLogger(), test.msg)
			if len(test.expectedErr) > 0 {
				require.NotNil(t, err)
				require.True(t, strings.Contains(err.Error(), test.expectedErr))
			} else {
				require.NoError(t, err)
			}
			if test.updated != nil {
				require.NotNil(t, updated)
			} else {
				require.Nil(t, updated)
			}
		})
	}
}

func newInMemDb() basedb.IDb {
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	return db
}

// SignMsg signs the given message by the given private key TODO redundant func from commit_test.go
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *message.ConsensusMessage, forkVersion string) *message.SignedMessage {
	//sigType := message.QBFTSigType
	//domain := message.ComputeSignatureDomain(message.PrimusTestnet, sigType)
	//sigRoot, err := message.ComputeSigningRoot(msg, domain, forksprotocol.GenesisForkVersion.String())
	sigRoot, err := msg.GetRoot()
	require.NoError(t, err)
	sig := sk.SignByte(sigRoot)

	return &message.SignedMessage{
		Message:   msg,
		Signers:   []message.OperatorID{message.OperatorID(id)},
		Signature: sig.Serialize(),
	}
}

// AggregateMessages will aggregate given msgs or return error TODO redundant func from commit_test.go
func AggregateMessages(sigs []*message.SignedMessage) (*message.SignedMessage, error) {
	var decided *message.SignedMessage
	var err error
	for _, msg := range sigs {
		if decided == nil {
			decided = msg.DeepCopy()
			if err != nil {
				return nil, errors.Wrap(err, "could not copy message")
			}
		} else {
			if err := decided.Aggregate(msg); err != nil {
				return nil, errors.Wrap(err, "could not aggregate message")
			}
		}
	}

	if decided == nil {
		return nil, errors.New("could not aggregate decided messages, no msgs")
	}

	return decided, nil
}

// GenerateNodes generates randomly nodes TODO redundant func from commit_test.go
func GenerateNodes(cnt int) (map[message.OperatorID]*bls.SecretKey, map[message.OperatorID]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[message.OperatorID]*proto.Node)
	sks := make(map[message.OperatorID]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[message.OperatorID(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[message.OperatorID(i)] = sk
	}
	return sks, nodes
}
