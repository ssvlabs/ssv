package eventdatahandler_test

import (
	"context"
	"testing"

	"github.com/bloxapp/ssv/ekm"
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
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/golang/mock/gomock"
	"github.com/prysmaticlabs/prysm/v4/testing/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestHandleBlockEventsStream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eb := eventbatcher.NewEventBatcher()

	events := []ethtypes.Log{
		{
			BlockNumber: 1,
			TxHash:      ethcommon.Hash{1},
		},
		{
			BlockNumber: 1,
			TxHash:      ethcommon.Hash{2},
		},
		{
			BlockNumber: 2,
			TxHash:      ethcommon.Hash{3},
		},
	}



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
	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{})

	edh := eventdatahandler.New(eventDB,
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