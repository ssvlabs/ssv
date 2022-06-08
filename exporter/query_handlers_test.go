package exporter

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestHandleUnknownQuery(t *testing.T) {
	logger := zap.L()

	nm := api.NetworkMessage{
		Msg: api.Message{
			Type:   "unknown_type",
			Filter: api.MessageFilter{},
		},
		Err:  nil,
		Conn: nil,
	}

	handleUnknownQuery(logger, &nm)
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
			nm := api.NetworkMessage{
				Msg: api.Message{
					Type:   api.TypeError,
					Filter: api.MessageFilter{},
				},
				Err:  test.netErr,
				Conn: nil,
			}
			handleErrorQuery(logger, &nm)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, test.expectedErr, errs[0])
		})
	}
}

func TestHandleOperatorsQuery(t *testing.T) {
	db, l, done := newDBAndLoggerForTest()
	defer done()
	s, _ := newStorageForTest(db, l)

	ois := []registrystorage.OperatorInformation{
		{
			PublicKey:    "01010101",
			Name:         "my_operator1",
			OwnerAddress: common.Address{},
		}, {
			PublicKey:    "02020202",
			Name:         "my_operator2",
			OwnerAddress: common.Address{},
		}, {
			PublicKey:    "03030303",
			Name:         "my_operator3",
			OwnerAddress: common.Address{},
		},
	}
	for _, oi := range ois {
		err := s.SaveOperatorInformation(&oi)
		require.NoError(t, err)
	}

	t.Run("query by index", func(t *testing.T) {
		nm := api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeOperator,
				Filter: api.MessageFilter{From: 0, To: 1},
			},
			Err:  nil,
			Conn: nil,
		}
		handleOperatorsQuery(l, s, &nm)
		require.Equal(t, api.TypeOperator, nm.Msg.Type)
		results, ok := nm.Msg.Data.([]registrystorage.OperatorInformation)
		require.True(t, ok)
		require.Equal(t, 2, len(results))
		for _, op := range results {
			require.True(t, strings.Contains(op.Name, "my_operator"))
			require.Less(t, op.Index, int64(2))
		}
	})

	t.Run("query non-existing index", func(t *testing.T) {
		nm := api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeOperator,
				Filter: api.MessageFilter{From: 100, To: 101},
			},
			Err:  nil,
			Conn: nil,
		}
		handleOperatorsQuery(l, s, &nm)
		require.Equal(t, api.TypeOperator, nm.Msg.Type)
		results, ok := nm.Msg.Data.([]registrystorage.OperatorInformation)
		require.True(t, ok)
		require.Equal(t, 0, len(results))
	})

	t.Run("query by pubKey", func(t *testing.T) {
		nm := api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeOperator,
				Filter: api.MessageFilter{PublicKey: "03030303"},
			},
			Err:  nil,
			Conn: nil,
		}
		handleOperatorsQuery(l, s, &nm)
		require.Equal(t, api.TypeOperator, nm.Msg.Type)
		results, ok := nm.Msg.Data.([]registrystorage.OperatorInformation)
		require.True(t, ok)
		require.Equal(t, 1, len(results))
		require.Equal(t, "my_operator3", results[0].Name)
		require.Equal(t, "03030303", results[0].PublicKey)
		require.Equal(t, int64(2), results[0].Index)
	})
}

func TestHandleValidatorsQuery(t *testing.T) {
	db, l, done := newDBAndLoggerForTest()
	defer done()
	s, _ := newStorageForTest(db, l)

	vis := []storage.ValidatorInformation{
		{
			PublicKey: "01010101",
			Operators: getMockOperatorLinks(),
		}, {
			PublicKey: "02020202",
			Operators: getMockOperatorLinks(),
		}, {
			PublicKey: "03030303",
			Operators: getMockOperatorLinks(),
		},
	}
	for _, vi := range vis {
		err := s.SaveValidatorInformation(&vi)
		require.NoError(t, err)
	}

	t.Run("query by index", func(t *testing.T) {
		nm := api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeValidator,
				Filter: api.MessageFilter{From: 0, To: 1},
			},
			Err:  nil,
			Conn: nil,
		}
		handleValidatorsQuery(l, s, &nm)
		require.Equal(t, api.TypeValidator, nm.Msg.Type)
		results, ok := nm.Msg.Data.([]storage.ValidatorInformation)
		require.True(t, ok)
		require.Equal(t, 2, len(results))
		for i, v := range results {
			pki := i + 1
			pk := fmt.Sprintf("0%d0%d0%d0%d", pki, pki, pki, pki)
			require.Equal(t, pk, v.PublicKey)
			require.Less(t, v.Index, int64(2))
		}
	})

	t.Run("query non-existing index", func(t *testing.T) {
		nm := api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeValidator,
				Filter: api.MessageFilter{From: 100, To: 101},
			},
			Err:  nil,
			Conn: nil,
		}
		handleValidatorsQuery(l, s, &nm)
		require.Equal(t, api.TypeValidator, nm.Msg.Type)
		results, ok := nm.Msg.Data.([]storage.ValidatorInformation)
		require.True(t, ok)
		require.Equal(t, 0, len(results))
	})

	t.Run("query by pubKey", func(t *testing.T) {
		nm := api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeValidator,
				Filter: api.MessageFilter{PublicKey: "03030303"},
			},
			Err:  nil,
			Conn: nil,
		}
		handleValidatorsQuery(l, s, &nm)
		require.Equal(t, api.TypeValidator, nm.Msg.Type)
		results, ok := nm.Msg.Data.([]storage.ValidatorInformation)
		require.True(t, ok)
		require.Equal(t, 1, len(results))
		require.Equal(t, int64(2), results[0].Index)
		require.Equal(t, "03030303", results[0].PublicKey)
	})
}

func TestHandleDecidedQuery(t *testing.T) {
	//db, l, done := newDBAndLoggerForTest()
	//defer done()
	//exporterStorage, ibftStorage := newStorageForTest(db, l)
	//_ = bls.Init(bls.BLS12_381)
	//
	//sks, _ := sync.GenerateNodes(4)
	//pk := sks[1].GetPublicKey()
	//identifier := format.IdentifierFormat(pk.Serialize(), message.RoleTypeAttester.String())
	//decided250Seq := sync.DecidedArr(t, 250, sks, []byte(identifier))
	//
	//// save decided
	//for _, d := range decided250Seq {
	//	require.NoError(t, ibftStorage.SaveDecided(d))
	//}
	//require.NoError(t, exporterStorage.SaveValidatorInformation(&storage.ValidatorInformation{
	//	PublicKey: pk.SerializeToHexStr(),
	//}))
	//
	//t.Run("valid range", func(t *testing.T) {
	//	nm := newDecidedAPIMsg(pk.SerializeToHexStr(), 0, 250)
	//	handleDecidedQuery(l, exporterStorage, ibftStorage, nm)
	//	require.NotNil(t, nm.Msg.Data)
	//	msgs, ok := nm.Msg.Data.([]*proto.SignedMessage)
	//	require.True(t, ok)
	//	require.Equal(t, 251, len(msgs)) // seq 0 - 250
	//})

	//t.Run("invalid range", func(t *testing.T) {
	//	nm := newDecidedAPIMsg(pk.SerializeToHexStr(), 400, 404)
	//	handleDecidedQuery(l, exporterStorage, ibftStorage, nm)
	//	require.NotNil(t, nm.Msg.Data)
	//	msgs, ok := nm.Msg.Data.([]*proto.SignedMessage)
	//	require.True(t, ok)
	//	require.Equal(t, 0, len(msgs)) // seq 0 - 250
	//})
	//
	//t.Run("non-exist validator", func(t *testing.T) {
	//	nm := newDecidedAPIMsg("xxx", 400, 404)
	//	handleDecidedQuery(l, exporterStorage, ibftStorage, nm)
	//	require.NotNil(t, nm.Msg.Data)
	//	errs, ok := nm.Msg.Data.([]string)
	//	require.True(t, ok)
	//	require.Equal(t, "internal error - could not find validator", errs[0])
	//})
}

// TODO: un-lint
//nolint
func newDecidedAPIMsg(pk string, from, to int64) *api.NetworkMessage {
	return &api.NetworkMessage{
		Msg: api.Message{
			Type: api.TypeDecided,
			Filter: api.MessageFilter{
				PublicKey: pk,
				From:      from,
				To:        to,
				Role:      api.RoleAttester,
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
	sExporter := storage.NewExporterStorage(db, logger)
	sIbft := qbftstorage.NewQBFTStore(db, logger, "attestation")
	return sExporter, sIbft
}

func getMockOperatorLinks() []storage.OperatorNodeLink {
	return []storage.OperatorNodeLink{
		{
			ID:        1,
			PublicKey: hex.EncodeToString([]byte{2, 2, 2, 2}),
		},
		{
			ID:        2,
			PublicKey: hex.EncodeToString([]byte{2, 2, 2, 2}),
		},
		{
			ID:        3,
			PublicKey: hex.EncodeToString([]byte{3, 3, 3, 3}),
		},
		{
			ID:        4,
			PublicKey: hex.EncodeToString([]byte{4, 4, 4, 4}),
		},
	}
}
