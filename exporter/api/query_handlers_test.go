package api

import (
	"math"
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/operator/storage"
	protocoltesting "github.com/bloxapp/ssv/protocol/v2/testing"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
)

func TestHandleUnknownQuery(t *testing.T) {
	logger := logging.TestLogger(t)

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
	logger := logging.TestLogger(t)

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
		test := test
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
	logger := logging.TestLogger(t)

	db, l, done := newDBAndLoggerForTest(logger)
	defer done()

	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		// skipping spectypes.BNRoleSyncCommitteeContribution to test non-existing storage
	}
	_, ibftStorage := newStorageForTest(db, l, roles...)
	_ = bls.Init(bls.BLS12_381)

	sks, _ := GenerateNodes(4)
	oids := make([]spectypes.OperatorID, 0)
	for oid := range sks {
		oids = append(oids, oid)
	}

	role := spectypes.BNRoleAttester
	pk := sks[1].GetPublicKey()
	decided250Seq, err := protocoltesting.CreateMultipleStoredInstances(sks, specqbft.Height(0), specqbft.Height(250), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
		id := spectypes.NewMsgID(types.GetDefaultDomain(), pk.Serialize(), role)
		return oids, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: id[:],
			Root:       [32]byte{0x1, 0x2, 0x3},
		}
	})
	require.NoError(t, err)

	// save decided
	for _, d := range decided250Seq {
		require.NoError(t, ibftStorage.Get(role).SaveInstance(d))
	}

	t.Run("valid range", func(t *testing.T) {
		nm := newDecidedAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleAttester, 0, 250)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		msgs, ok := nm.Msg.Data.([]*SignedMessageAPI)
		require.True(t, ok, "expected []*SignedMessageAPI, got %+v", nm.Msg.Data)
		require.Equal(t, 251, len(msgs)) // seq 0 - 250
	})

	t.Run("invalid range", func(t *testing.T) {
		nm := newDecidedAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleAttester, 400, 404)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		data, ok := nm.Msg.Data.([]string)
		require.True(t, ok)
		require.Equal(t, []string{"no messages"}, data)
	})

	t.Run("non-existing validator", func(t *testing.T) {
		nm := newDecidedAPIMsg("xxx", spectypes.BNRoleAttester, 400, 404)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		errs, ok := nm.Msg.Data.([]string)
		require.True(t, ok)
		require.Equal(t, "internal error - could not read validator key", errs[0])
	})

	t.Run("non-existing role", func(t *testing.T) {
		nm := newDecidedAPIMsg(pk.SerializeToHexStr(), math.MaxUint64, 0, 250)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		errs, ok := nm.Msg.Data.([]string)
		require.True(t, ok)
		require.Equal(t, "role doesn't exist", errs[0])
	})

	t.Run("non-existing storage", func(t *testing.T) {
		nm := newDecidedAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleSyncCommitteeContribution, 0, 250)
		HandleDecidedQuery(l, ibftStorage, nm)
		require.NotNil(t, nm.Msg.Data)
		errs, ok := nm.Msg.Data.([]string)
		require.True(t, ok)
		require.Equal(t, "internal error - role storage doesn't exist", errs[0])
	})
}

func newDecidedAPIMsg(pk string, role spectypes.BeaconRole, from, to uint64) *NetworkMessage {
	return &NetworkMessage{
		Msg: Message{
			Type: TypeDecided,
			Filter: MessageFilter{
				PublicKey: pk,
				From:      from,
				To:        to,
				Role:      role.String(),
			},
		},
		Err:  nil,
		Conn: nil,
	}
}

func newDBAndLoggerForTest(logger *zap.Logger) (basedb.Database, *zap.Logger, func()) {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, nil, func() {}
	}
	return db, logger, func() {
		db.Close()
	}
}

func newStorageForTest(db basedb.Database, logger *zap.Logger, roles ...spectypes.BeaconRole) (storage.Storage, *qbftstorage.QBFTStores) {
	sExporter, err := storage.NewNodeStorage(logger, db)
	if err != nil {
		panic(err)
	}

	storageMap := qbftstorage.NewStores()
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(db, role.String()))
	}

	return sExporter, storageMap
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, []*spectypes.Operator) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make([]*spectypes.Operator, 0)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		opPubKey, _, err := rsaencryption.GenerateKeys()
		if err != nil {
			panic(err)
		}

		nodes = append(nodes, &spectypes.Operator{
			OperatorID:        spectypes.OperatorID(i),
			SharePubKey:       sk.GetPublicKey().Serialize(),
			SSVOperatorPubKey: opPubKey,
		})
		sks[spectypes.OperatorID(i)] = sk
	}
	return sks, nodes
}
