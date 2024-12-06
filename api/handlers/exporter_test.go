package handlers

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ssvlabs/ssv/api"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/utils/rsaencryption"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/convert"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleDecideds(t *testing.T) {
	logger := logging.TestLogger(t)

	db, l, done := newDBAndLoggerForTest(logger)
	defer done()

	roles := []convert.RunnerRole{
		convert.RoleAttester,
	}
	_, ibftStorage := newStorageForTest(db, l, roles...)
	_ = bls.Init(bls.BLS12_381)

	exporter := Exporter{
		DomainType: spectypes.DomainAttester,
		QBFTStores: ibftStorage,
	}

	sks, op, rsaKeys := GenerateNodes(4)
	oids := make([]spectypes.OperatorID, 0, len(op))
	for _, o := range op {
		oids = append(oids, o.OperatorID)
	}

	var msgID convert.MessageID
	for _, role := range roles {
		pk := sks[1].GetPublicKey()
		networkConfig, err := networkconfig.GetNetworkConfigByName(networkconfig.HoleskyStage.Name)
		require.NoError(t, err)
		decided250Seq, err := protocoltesting.CreateMultipleStoredInstances(rsaKeys, specqbft.Height(0), specqbft.Height(250), func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message) {
			msgID = convert.NewMsgID(networkConfig.DomainType, pk.Serialize(), role)
			return oids, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     height,
				Round:      1,
				Identifier: msgID[:],
				Root:       [32]byte{0x1, 0x2, 0x3},
			}
		})
		require.NoError(t, err)

		// save participants
		for _, d := range decided250Seq {
			_, err := ibftStorage.Get(role).UpdateParticipants(
				convert.MessageID(d.DecidedMessage.SSVMessage.MsgID),
				phase0.Slot(d.State.Height),
				d.DecidedMessage.OperatorIDs,
			)
			require.NoError(t, err)
		}

		var request = struct {
			From    uint64        `json:"from"`
			To      uint64        `json:"to"`
			Roles   api.RoleSlice `json:"roles"`
			PubKeys api.HexSlice  `json:"pubkeys"`
		}{
			From:    0,
			To:      250,
			Roles:   []api.Role{api.Role(spectypes.BNRoleAttester)},
			PubKeys: api.HexSlice{msgID[:]},
		}

		data, _ := json.Marshal(request)

		req := httptest.NewRequest(http.MethodPost, "/decideds", bytes.NewBuffer(data))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		err = exporter.Decideds(w, req)
		require.NoError(t, err)

		res := w.Result()
		defer res.Body.Close()

		var response struct {
			Data []*ParticipantResponse `json:"data"`
		}

		data, err = io.ReadAll(res.Body)
		require.NoError(t, err)

		err = json.Unmarshal(data, &response)
		require.NoError(t, err)

		assert.Len(t, response.Data, 251)
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

func newStorageForTest(db basedb.Database, logger *zap.Logger, roles ...convert.RunnerRole) (storage.Storage, *qbftstorage.QBFTStores) {
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
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, []*spectypes.Operator, []*rsa.PrivateKey) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make([]*spectypes.Operator, 0, cnt)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	rsaKeys := make([]*rsa.PrivateKey, 0, cnt)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		opPubKey, privateKey, err := rsaencryption.GenerateKeys()
		if err != nil {
			panic(err)
		}
		pk, err := rsaencryption.PemToPrivateKey(privateKey)
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
