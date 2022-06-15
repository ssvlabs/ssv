package api

import (
	"fmt"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	protocoltesting "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestHandleUnknownQuery(t *testing.T) {
	logger := zap.L()

	nm := NetworkMessage{
		Msg: Message{
			Type:   "unknown_type",
			Filter: MessageFilter{},
		},
		Err:  nil,
		Conn: nil,
	}

	HandleUnknownQuery(logger, &nm)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok)
	require.Equal(t, "bad request - unknown message type 'unknown_type'", errs[0])
}

func TestHandleErrorQuery(t *testing.T) {
	logger := zap.L()

	tests := []struct {
		expectedErr string
		netErr      error
		name        string
	}{
		{
			"dummy",
			errors.New("dummy"),
			"network error",
		},
		{
			unknownError,
			nil,
			"unknown error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nm := NetworkMessage{
				Msg: Message{
					Type:   TypeError,
					Filter: MessageFilter{},
				},
				Err:  test.netErr,
				Conn: nil,
			}
			HandleErrorQuery(logger, &nm)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, test.expectedErr, errs[0])
		})
	}
}

func TestHandleDecidedQuery(t *testing.T) {
	logex.Build("TestHandleDecidedQuery", zapcore.DebugLevel, nil)

	db, l, done := newDBAndLoggerForTest()
	defer done()
	exporterStorage, ibftStorage := newStorageForTest(db, l)
	_ = bls.Init(bls.BLS12_381)

	sks, _ := validator.GenerateNodes(4)
	oids := make([]message.OperatorID, 0)
	for oid := range sks {
		oids = append(oids, oid)
	}

	pk := sks[1].GetPublicKey()
	decided250Seq, err := protocoltesting.CreateMultipleSignedMessages(sks, message.Height(0), message.Height(250), func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
		commitData := message.CommitData{Data: []byte(fmt.Sprintf("msg-data-%d", height))}
		commitDataBytes, err := commitData.Encode()
		if err != nil {
			panic(err)
		}

		return oids, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: message.NewIdentifier(pk.Serialize(), message.RoleTypeAttester),
			Data:       commitDataBytes,
		}
	})
	require.NoError(t, err)

	// save decided
	for _, d := range decided250Seq {
		require.NoError(t, ibftStorage.SaveDecided(d))
	}
	require.NoError(t, exporterStorage.SaveValidatorInformation(&storage.ValidatorInformation{
		PublicKey: pk.SerializeToHexStr(),
	}))

	t.Run("valid range", func(t *testing.T) {
		nm := newDecidedAPIMsg(pk.SerializeToHexStr(), 0, 250)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		msgs, ok := nm.Msg.Data.([]*message.SignedMessage)
		require.True(t, ok)
		require.Equal(t, 251, len(msgs)) // seq 0 - 250
	})

	t.Run("invalid range", func(t *testing.T) {
		nm := newDecidedAPIMsg(pk.SerializeToHexStr(), 400, 404)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		msgs, ok := nm.Msg.Data.([]*message.SignedMessage)
		require.True(t, ok)
		require.Equal(t, 0, len(msgs)) // seq 0 - 250
	})

	t.Run("non-exist validator", func(t *testing.T) {
		nm := newDecidedAPIMsg("xxx", 400, 404)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		errs, ok := nm.Msg.Data.([]string)
		require.True(t, ok)
		require.Equal(t, "internal error - could not read validator key", errs[0])
	})
}

func newDecidedAPIMsg(pk string, from, to uint64) *NetworkMessage {
	return &NetworkMessage{
		Msg: Message{
			Type: TypeDecided,
			Filter: MessageFilter{
				PublicKey: pk,
				From:      from,
				To:        to,
				Role:      RoleAttester,
			},
		},
		Err:  nil,
		Conn: nil,
	}
}

func newDBAndLoggerForTest() (basedb.IDb, *zap.Logger, func()) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, nil, func() {}
	}
	return db, logger, func() {
		db.Close()
	}
}

func newStorageForTest(db basedb.IDb, logger *zap.Logger) (storage.Storage, qbftstorage.QBFTStore) {
	sExporter := storage.NewNodeStorage(db, logger)
	sIbft := qbftstorage.NewQBFTStore(db, logger, "attestation")
	return sExporter, sIbft
}
