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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleDecideds(t *testing.T) {
	logger := logging.TestLogger(t)

	db, l, done := newDBAndLoggerForTest(logger)
	defer done()

	roles := []convert.RunnerRole{
		convert.RoleAttester,
		// skipping spectypes.BNRoleSyncCommitteeContribution to test non-existing storage
	}
	_, ibftStorage := newStorageForTest(db, l, roles...)
	_ = bls.Init(bls.BLS12_381)

	exporter := Exporter{
		DomainType: spectypes.DomainAttester,
		QBFTStores: ibftStorage,
		Log:        zap.NewNop(),
	}

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
			id := convert.NewMsgID(networkConfig.DomainType, pk.Serialize(), role)
			return oids, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     height,
				Round:      1,
				Identifier: id[:],
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
			From:  0,
			To:    11,
			Roles: []api.Role{api.Role(spectypes.BNRoleAttester)},
			// PubKeys: pk[:],
		}

		data, _ := json.Marshal(request)

		req := httptest.NewRequest(http.MethodPost, "/decideds", bytes.NewBuffer(data))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		exporter.Decideds(w, req)
		res := w.Result()
		defer res.Body.Close()

		data, err = io.ReadAll(res.Body)
		if err != nil {
			t.Errorf("expected error to be nil got %v", err)
		}
		if string(data) != "ABC" {
			t.Errorf("expected ABC got %v", string(data))
		}
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
