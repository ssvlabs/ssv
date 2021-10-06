package ibft

import (
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	ibftsync "github.com/bloxapp/ssv/ibft/sync"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strings"
	"testing"
)

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

	sks, committee := ibftsync.GenerateNodes(4)
	pk := sks[1].GetPublicKey()
	for i, sk := range sks {
		require.NoError(t, cr.validatorStorage.SaveValidatorShare(&validatorstorage.Share{
			NodeID:    i + 1,
			PublicKey: pk,
			ShareKey:  sk,
			Committee: committee,
			Metadata:  nil,
		}))
	}
	identifier := format.IdentifierFormat(pk.Serialize(), beacon.RoleTypeAttester.String())
	decided25Seq := ibftsync.DecidedArr(t, 25, sks, []byte(identifier))

	// save decided
	for _, d := range decided25Seq {
		require.NoError(t, cr.ibftStorage.SaveDecided(d))
	}

	// TODO: add test for valid message

	t.Run("different message value", func(t *testing.T) {
		commitMsg := signMsg(t, 4, sks[4], &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     1,
			SeqNumber: 25,
			Lambda:    decided25Seq[25].Message.GetLambda(),
		})
		err := cr.onCommitMessage(commitMsg)
		require.NotNil(t, err)
		require.True(t, strings.Contains(err.Error(), "can't aggregate different messages"))
	})

	t.Run("non-valid lambda", func(t *testing.T) {
		commitMsg := signMsg(t, 4, sks[4], &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     1,
			SeqNumber: 25,
			Lambda:    []byte("xxx_ATTESTER"),
		})
		err := cr.onCommitMessage(commitMsg)
		require.NotNil(t, err)
		require.True(t, strings.Contains(err.Error(), "could not read public key"))
	})

	t.Run("share not found", func(t *testing.T) {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		commitMsg := signMsg(t, 4, sk, &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     1,
			SeqNumber: 25,
			Lambda:    []byte(format.IdentifierFormat(sk.GetPublicKey().Serialize(), beacon.RoleTypeAttester.String())),
		})
		err := cr.onCommitMessage(commitMsg)
		require.Nil(t, err)
	})

	t.Run("invalid message", func(t *testing.T) {
		commitMsg := signMsg(t, 4, sks[4], &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     1,
			SeqNumber: 25,
			Lambda:    decided25Seq[25].Message.GetLambda(),
		})
		commitMsg.Signature = []byte("dummy")
		err := cr.onCommitMessage(commitMsg)
		require.NotNil(t, err)
		fmt.Println(err.Error())
		require.True(t, strings.Contains(err.Error(), "invalid commit message"))
	})
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
