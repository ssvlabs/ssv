package ibft

import (
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/ibft/proto"
	ibftsync "github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/validator/types"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/prysm/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestCommitReader_onMessage(t *testing.T) {
	_ = bls.Init(bls.BLS12_381)
	reader := setupReaderForTest(t)
	cr := reader.(*commitReader)

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	t.Run("invalid message", func(t *testing.T) {
		require.False(t, cr.onMessage(&proto.SignedMessage{
			Message:   nil,
			Signature: []byte{},
			SignerIds: []uint64{1},
		}))
	})

	t.Run("non-commit", func(t *testing.T) {
		msg := signMsg(t, 4, sk, &proto.Message{
			Type:      proto.RoundState_Prepare,
			Round:     1,
			SeqNumber: 25,
			Lambda:    []byte(format.IdentifierFormat(sk.GetPublicKey().Serialize(), beacon.RoleTypeAttester.String())),
		})
		require.False(t, cr.onMessage(msg))
	})
}

func TestCommitReader_onCommitMessage(t *testing.T) {
	_ = bls.Init(bls.BLS12_381)
	reader := setupReaderForTest(t)
	cr := reader.(*commitReader)
	cn := make(chan api.Message)
	sub := cr.out.Subscribe(cn)
	defer sub.Unsubscribe()

	var incoming []api.Message
	var mut sync.Mutex
	go func() {
		for netMsg := range cn {
			mut.Lock()
			incoming = append(incoming, netMsg)
			mut.Unlock()
		}
	}()

	sks, committee := ibftsync.GenerateNodes(4)
	pk := sks[1].GetPublicKey()
	require.NoError(t, cr.validatorStorage.SaveValidatorShare(&types.Share{
		NodeID:    1,
		PublicKey: pk,
		Committee: committee,
		Metadata:  nil,
	}))
	identifier := format.IdentifierFormat(pk.Serialize(), beacon.RoleTypeAttester.String())
	var sigs []*proto.SignedMessage
	for i := 1; i < 4; i++ {
		sigs = append(sigs, signMsg(t, uint64(i), sks[uint64(i)], &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     1,
			SeqNumber: 1,
			Lambda:    []byte(identifier),
			Value:     []byte("value"),
		}))
	}
	decided, err := proto.AggregateMessages(sigs)
	require.NoError(t, err)

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	tests := []struct {
		name        string
		expectedErr string
		msg         *proto.SignedMessage
		after       func(t *testing.T)
	}{
		{
			"valid",
			"",
			signMsg(t, uint64(4), sks[uint64(4)], &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     1,
				SeqNumber: 1,
				Lambda:    []byte(identifier),
				Value:     []byte("value"),
			}),
			func(t *testing.T) {
				updated, found, err := cr.ibftStorage.GetDecided([]byte(identifier), uint64(1))
				require.Nil(t, err)
				require.True(t, found)
				require.Equal(t, 4, len(updated.SignerIds))
			},
		},
		{
			"different value",
			"can't aggregate different messages",
			signMsg(t, uint64(4), sks[uint64(4)], &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     1,
				SeqNumber: 1,
				Lambda:    []byte(identifier),
				Value:     []byte("xxx"),
			}),
			nil,
		},
		{
			"invalid lambda",
			"could not read public key",
			signMsg(t, 4, sks[4], &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     1,
				SeqNumber: 1,
				Lambda:    []byte("xxx_ATTESTER"),
			}),
			nil,
		},
		{
			"share not found",
			"",
			signMsg(t, 4, sk, &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     1,
				SeqNumber: 25,
				Lambda:    []byte(format.IdentifierFormat(sk.GetPublicKey().Serialize(), beacon.RoleTypeAttester.String())),
			}),
			nil,
		},
		{
			"invalid message",
			"invalid commit message",
			func() *proto.SignedMessage {
				commitMsg := signMsg(t, 4, sks[4], &proto.Message{
					Type:      proto.RoundState_Commit,
					Round:     1,
					SeqNumber: 1,
					Lambda:    []byte(identifier),
				})
				commitMsg.Signature = []byte("dummy")
				return commitMsg
			}(),
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, cr.ibftStorage.SaveDecided(decided))
			err := cr.onCommitMessage(test.msg)
			if len(test.expectedErr) > 0 {
				require.NotNil(t, err)
				require.True(t, strings.Contains(err.Error(), test.expectedErr))
			} else {
				require.Nil(t, err)
			}
			if test.after != nil {
				test.after(t)
			}
		})
	}

	time.Sleep(10 * time.Millisecond)
	mut.Lock()
	defer mut.Unlock()
	require.Equal(t, 1, len(incoming))
}

func setupReaderForTest(t *testing.T) Reader {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	require.NoError(t, err)
	validatorStorage := validatorstorage.NewCollection(validatorstorage.CollectionOptions{
		DB:     db,
		Logger: logger,
	})
	ibftStorage := collections.NewIbft(db, logger, "attestation")
	_ = bls.Init(bls.BLS12_381)

	cr := NewCommitReader(CommitReaderOptions{
		Logger:           logger,
		Network:          nil,
		ValidatorStorage: validatorStorage,
		IbftStorage:      &ibftStorage,
		Out:              new(event.Feed),
	})

	return cr
}

// signMsg signs the given message by the given private key
func signMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := msg.Sign(sk)
	require.NoError(t, err)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}
