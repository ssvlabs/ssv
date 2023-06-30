package eventdatahandler_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdatahandler"
	"github.com/bloxapp/ssv/eth/eventdb"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/golang/mock/gomock"
	"github.com/prysmaticlabs/prysm/v4/testing/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestHandleBlockEventsStream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eb := eventbatcher.NewEventBatcher()

	var rawOperatorAdded = `{
		"address": "0x3A23a7F455E853058d900f5dc86f1Bb1589b54F9",
		"topics": [
		  "0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4",
		  "0x0000000000000000000000000000000000000000000000000000000000000001",
		  "0x0000000000000000000000009d4d2d2dd7f11953535691786690610512e26b6c"
		],
		"data": "0x0000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000deb9cd9e0000000000000000000000000000000000000000000000000000000000000002c0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000002644c5330744c5331435255644a54694253553045675546564354456c4449457446575330744c533074436b314a53554a4a616b464f516d64726357687261556335647a424351564646526b464254304e425554684254556c4a516b4e6e53304e42555556424d5667324d5546585930303151554e4c61474e354d546c556145494b627939484d576c684e3142794f565572616c4a35615759355a6a4179524739736430393156325a4c4c7a645356556c684f45684562484276516c564552446b77525456515547644a5379397354584234527974586277707751324e3562544270576b395554304a7a4e44453562456833547a41346258466a61314a735a4567355745786d626d59325554687157465235596d3179597a64574e6d77794e56707263546c3455306f7762485231436e646d546e5654537a4e435a6e46744e6b51784f55593061545643626d56615357686a52564a54596c464c5744467862574e71596e5a464c326379516b6f34547a68615a5567726430527a54484a694e6e5a585156494b5933425957473175656c4533566c70365a6b6c485447564c5655314354546836535730726358493452475a34534568536556553151544533634655346379394d4e5570355258453152474a6a6332513264486c6e625170355545394259554e7a576c6456524549335547684c4f487055575539575969394d4d316c6e53545534626a4658656b35494d307335636d467265557070546d55785445394756565a7a51544644556e68745132597a436d6c525355524255554643436930744c5330745255354549464a545153425156554a4d53554d675330565a4c5330744c53304b00000000000000000000000000000000000000000000000000000000",
		"blockNumber": "0x843735",
		"transactionHash": "0x4f4f9c1e37cf0800a201227e8fa3cad6f8f246ac1cca1cb90e2c3311538b300c"
	  }`

	LogOperatorAdded := unmarshalLog(t, rawOperatorAdded)

	events := []ethtypes.Log{}

	events = append(events, *LogOperatorAdded)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := eventdb.NewEventDB(db.(*kv.BadgerDb).Badger())
	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db)
	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, networkconfig.NetworkConfig{}, true)
	if err != nil {
		logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bc := beacon.NewMockBeaconNode(ctrl)
	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{
		Context:         ctx,
		DB:              db,
		RegistryStorage: nodeStorage,
	})

	edh := eventdatahandler.New(
		eventDB,
		validatorCtrl,
		operatorData,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		eventdatahandler.WithFullNode(),
		eventdatahandler.WithLogger(logger))

	blockEvents := eb.BatchHistoricalEvents(events)
	err = edh.HandleBlockEventsStream(blockEvents)
	require.NoError(t, err)
}

func setupOperatorStorage(logger *zap.Logger, db basedb.IDb) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}
	operatorPubKey, err := nodeStorage.SetupPrivateKey(logger, "", true)
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(logger, operatorPubKey)
	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: operatorPubKey,
		}
	}

	return nodeStorage, operatorData
}

func unmarshalLog(t *testing.T, rawOperatorAdded string) *types.Log {
	var vLogOperatorAdded types.Log
	err := json.Unmarshal([]byte(rawOperatorAdded), &vLogOperatorAdded)
	require.NoError(t, err)
	contractAbi, err := abi.JSON(strings.NewReader(contract.ContractABI))
	require.NoError(t, err)
	require.NotNil(t, contractAbi)
	return &vLogOperatorAdded
}
