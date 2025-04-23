package api

import (
	"crypto/rsa"
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/storage"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
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

	sks, op, rsaKeys := GenerateNodes(4)
	oids := make([]spectypes.OperatorID, 0, len(op))
	for _, o := range op {
		oids = append(oids, o.OperatorID)
	}

	for _, role := range roles {
		pk := sks[1].GetPublicKey()
		networkConfig, err := networkconfig.GetNetworkConfigByName(networkconfig.HoleskyStage.Name)
		require.NoError(t, err)
		decided250Seq, err := protocoltesting.CreateMultipleStoredInstances(rsaKeys, specqbft.Height(0), specqbft.Height(250), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
			return oids, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     height,
				Round:      1,
				Identifier: pk.Serialize(),
				Root:       [32]byte{0x1, 0x2, 0x3},
			}
		})
		require.NoError(t, err)

		// save participants
		for _, d := range decided250Seq {
			_, err := ibftStorage.Get(role).SaveParticipants(
				spectypes.ValidatorPK(pk.Serialize()),
				phase0.Slot(d.State.Height),
				d.DecidedMessage.OperatorIDs,
			)
			require.NoError(t, err)
		}

		t.Run("valid range", func(t *testing.T) {
			nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleAttester, 0, 250)
			HandleParticipantsQuery(l, ibftStorage, nm, networkConfig.DomainType)
			require.NotNil(t, nm.Msg.Data)
			msgs, ok := nm.Msg.Data.([]*ParticipantsAPI)

			require.True(t, ok, "expected []*ParticipantsAPI, got %+v", nm.Msg.Data)
			require.Equal(t, 251, len(msgs)) // seq 0 - 250
		})

		t.Run("invalid range", func(t *testing.T) {
			nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleAttester, 400, 404)
			HandleParticipantsQuery(l, ibftStorage, nm, networkConfig.DomainType)
			require.NotNil(t, nm.Msg.Data)
			data, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, []string{"no messages"}, data)
		})

		t.Run("non-existing validator", func(t *testing.T) {
			nm := newParticipantsAPIMsg("xxx", spectypes.BNRoleAttester, 400, 404)
			HandleParticipantsQuery(l, ibftStorage, nm, networkConfig.DomainType)
			require.NotNil(t, nm.Msg.Data)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, "internal error - could not read validator key", errs[0])
		})

		t.Run("non-existing role", func(t *testing.T) {
			nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), math.MaxUint64, 0, 250)
			HandleParticipantsQuery(l, ibftStorage, nm, networkConfig.DomainType)
			require.NotNil(t, nm.Msg.Data)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, "role doesn't exist", errs[0])
		})

		t.Run("non-existing storage", func(t *testing.T) {
			nm := newParticipantsAPIMsg(pk.SerializeToHexStr(), spectypes.BNRoleSyncCommitteeContribution, 0, 250)
			HandleParticipantsQuery(l, ibftStorage, nm, networkConfig.DomainType)
			require.NotNil(t, nm.Msg.Data)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, "internal error - role storage doesn't exist", errs[0])
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
	sExporter, err := storage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
	if err != nil {
		panic(err)
	}

	storageMap := qbftstorage.NewStores()
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(db, role))
	}

	return sExporter, storageMap
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, []*spectypes.Operator, []*rsa.PrivateKey) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make([]*spectypes.Operator, 0, cnt)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	rsaKeys := make([]*rsa.PrivateKey, 0, cnt)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		opPubKey, privateKey, err := rsaencryption.GenerateKeyPairPEM()
		if err != nil {
			panic(err)
		}
		pk, err := rsaencryption.PEMToPrivateKey(privateKey)
		if err != nil {
			panic(err)
		}

		nodes = append(nodes, &spectypes.Operator{
			OperatorID:        spectypes.OperatorID(i),
			SSVOperatorPubKey: opPubKey,
		})
		sks[spectypes.OperatorID(i)] = sk
		rsaKeys = append(rsaKeys, pk)
	}
	return sks, nodes, rsaKeys
}
