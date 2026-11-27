package api

import (
	"errors"
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestHandleUnknownQuery(t *testing.T) {
	logger := log.TestLogger(t)

	nm := NetworkMessage{
		Msg: Message{
			Type:   "unknown_type",
			Filter: MessageFilter{},
		},
		Err:  nil,
		Conn: nil,
	}

	h := NewHandler(logger)
	h.HandleUnknownQuery(&nm)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok)
	require.Equal(t, "bad request - unknown message type 'unknown_type'", errs[0])
}

func TestHandleErrorQuery(t *testing.T) {
	logger := log.TestLogger(t)

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
			h := NewHandler(logger)
			h.HandleErrorQuery(&nm)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, test.expectedErr, errs[0])
		})
	}
}

func TestHandleDecidedQuery(t *testing.T) {
	logger := log.TestLogger(t)

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

	sks, oids := GenerateNodes(4)

	pk := sks[1].GetPublicKey()
	for _, role := range roles {
		for slot := phase0.Slot(0); slot <= 250; slot++ {
			_, err := ibftStorage.Get(role).SaveParticipants(
				spectypes.ValidatorPK(pk.Serialize()),
				slot,
				oids,
			)
			require.NoError(t, err)
		}

		t.Run(role.String(), func(t *testing.T) {
			t.Run("valid range", func(t *testing.T) {
				nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), role, 0, 250)
				h := NewHandler(l)
				h.HandleParticipantsQuery(ibftStorage, nm, networkconfig.TestNetwork)
				require.NotNil(t, nm.Msg.Data)
				msgs, ok := nm.Msg.Data.([]*ParticipantsAPI)

				require.True(t, ok, "expected []*ParticipantsAPI, got %+v", nm.Msg.Data)
				require.Equal(t, 251, len(msgs)) // seq 0 - 250
			})

			t.Run("invalid range", func(t *testing.T) {
				nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), role, 400, 404)
				h := NewHandler(l)
				h.HandleParticipantsQuery(ibftStorage, nm, networkconfig.TestNetwork)
				require.NotNil(t, nm.Msg.Data)
				data, ok := nm.Msg.Data.([]string)
				require.True(t, ok)
				require.Equal(t, []string{"no messages"}, data)
			})

			t.Run("non-existing validator", func(t *testing.T) {
				nm := newParticipantsAPIMsg("xxx", role, 400, 404)
				h := NewHandler(l)
				h.HandleParticipantsQuery(ibftStorage, nm, networkconfig.TestNetwork)
				require.NotNil(t, nm.Msg.Data)
				errs, ok := nm.Msg.Data.([]string)
				require.True(t, ok)
				require.Equal(t, "internal error - could not read validator key", errs[0])
			})

			t.Run("non-existing role", func(t *testing.T) {
				nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), math.MaxUint64, 0, 250)
				h := NewHandler(l)
				h.HandleParticipantsQuery(ibftStorage, nm, networkconfig.TestNetwork)
				require.NotNil(t, nm.Msg.Data)
				errs, ok := nm.Msg.Data.([]string)
				require.True(t, ok)
				require.Equal(t, "role doesn't exist", errs[0])
			})

			t.Run("non-existing storage", func(t *testing.T) {
				nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleSyncCommitteeContribution, 0, 250)
				h := NewHandler(l)
				h.HandleParticipantsQuery(ibftStorage, nm, networkconfig.TestNetwork)
				require.NotNil(t, nm.Msg.Data)
				errs, ok := nm.Msg.Data.([]string)
				require.True(t, ok)
				require.Equal(t, "internal error - role storage doesn't exist", errs[0])
			})
		})
	}
}

func newParticipantsAPIMsg(pk string, role spectypes.BeaconRole, from, to uint64) *NetworkMessage {
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

func newStorageForTest(db basedb.Database, logger *zap.Logger, roles ...spectypes.BeaconRole) (storage.Storage, *qbftstorage.ParticipantStores) {
	sExporter, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
	if err != nil {
		panic(err)
	}

	storageMap := qbftstorage.NewStores()
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(logger, db, role))
	}

	return sExporter, storageMap
}

// GenerateNodes returns the operator IDs 1..cnt and a random BLS secret key for each.
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, []spectypes.OperatorID) {
	_ = bls.Init(bls.BLS12_381)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey, cnt)
	oids := make([]spectypes.OperatorID, 0, cnt)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		sks[spectypes.OperatorID(i)] = sk
		oids = append(oids, spectypes.OperatorID(i))
	}
	return sks, oids
}
