package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"strings"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestProcessLateCommitMsg(t *testing.T) {
	sks, _ := GenerateNodes(4)
	db := qbftstorage.NewQBFTStore(newInMemDb(), zap.L(), "attestation")

	share := beacon.Share{}
	share.PublicKey = sks[1].GetPublicKey()
	share.Committee = make(map[spectypes.OperatorID]*beacon.Node, 4)
	identifier := spectypes.NewMsgID(share.PublicKey.Serialize(), spectypes.BNRoleAttester) // TODO should using fork to get identifier?

	ctrl := Controller{
		ValidatorShare: &beacon.Share{
			NodeID:    1,
			PublicKey: sks[1].GetPublicKey(),
			Committee: nil,
		},
	}
	ctrl.DecidedFactory = factory.NewDecidedFactory(logex.GetLogger(), strategy.ModeFullNode, db, nil)
	ctrl.DecidedStrategy = ctrl.DecidedFactory.GetStrategy()

	var sigs []*specqbft.SignedMessage
	commitData, err := (&specqbft.CommitData{Data: []byte("value")}).Encode()
	require.NoError(t, err)

	for i := 1; i < 4; i++ {
		sigs = append(sigs, SignMsg(t, uint64(i), sks[spectypes.OperatorID(i)], &specqbft.Message{
			Height:     2,
			MsgType:    specqbft.CommitMsgType,
			Round:      3,
			Identifier: identifier[:],
			Data:       commitData,
		}, forksprotocol.GenesisForkVersion.String()))
	}
	decided, err := AggregateMessages(sigs)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expectedErr string
		updated     interface{}
		msg         *specqbft.SignedMessage
	}{
		{
			"valid",
			"",
			struct{}{},
			SignMsg(t, 4, sks[4], &specqbft.Message{
				Height:     specqbft.Height(2),
				MsgType:    specqbft.CommitMsgType,
				Round:      3,
				Identifier: identifier[:],
				Data:       commitData,
			}, forksprotocol.GenesisForkVersion.String()),
		},
		{
			"invalid",
			"could not aggregate commit message",
			nil,
			func() *specqbft.SignedMessage {
				msg := SignMsg(t, 4, sks[4], &specqbft.Message{
					Height:     2,
					MsgType:    specqbft.CommitMsgType,
					Round:      3,
					Identifier: identifier[:],
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
			SignMsg(t, 4, sks[4], &specqbft.Message{
				Height:     specqbft.Height(2),
				MsgType:    specqbft.CommitMsgType,
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
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *specqbft.Message, forkVersion string) *specqbft.SignedMessage {
	sigType := spectypes.QBFTSignatureType
	domain := spectypes.ComputeSignatureDomain(message.GetDefaultDomain(), sigType)
	sigRoot, err := spectypes.ComputeSigningRoot(msg, domain)
	require.NoError(t, err)
	sig := sk.SignByte(sigRoot)

	return &specqbft.SignedMessage{
		Message:   msg,
		Signers:   []spectypes.OperatorID{spectypes.OperatorID(id)},
		Signature: sig.Serialize(),
	}
}

// AggregateMessages will aggregate given msgs or return error TODO redundant func from commit_test.go
func AggregateMessages(sigs []*specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	var decided *specqbft.SignedMessage
	var err error
	for _, msg := range sigs {
		if decided == nil {
			decided = msg.DeepCopy()
			if err != nil {
				return nil, errors.Wrap(err, "could not copy message")
			}
		} else {
			if err := message.Aggregate(decided, msg); err != nil {
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
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, map[spectypes.OperatorID]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[spectypes.OperatorID]*beacon.Node)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[spectypes.OperatorID(i)] = &beacon.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[spectypes.OperatorID(i)] = sk
	}
	return sks, nodes
}
