package eventdispatcher

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdatahandler"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/simulator"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/blskeygen"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestEventDispatcher(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	blockStream := make(chan []*ethtypes.Block)
	defer close(blockStream)
	done := make(chan struct{})
	defer close(done)

	// Create sim instance with a delay between block execution
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node.RPCHandler()
	httpSrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpSrv.Close()

	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Check contract code at the simulated blockchain
	contractCode, err := sim.CodeAt(ctx, contractAddr, nil)
	if err != nil {
		t.Errorf("getting contract code: %v", err)
	}
	require.NotEmpty(t, contractCode)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim)
	require.NoError(t, err)

	addr := "ws:" + strings.TrimPrefix(httpSrv.URL, "http:")
	client, err := executionclient.New(ctx, addr, contractAddr, executionclient.WithLogger(logger))
	require.NoError(t, err)

	isReady, err := client.IsReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, isReady)

	// TODO: Pack operator public key to work with ABI decoder
	t.Skip()

	// Generate operator key
	_, operatorPubKey := blskeygen.GenBLSKeyPair()

	// operatorPublicKeyABI, err := contract.OperatorPublicKeyMetaData.GetAbi()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// key := []byte("LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBMVg2MUFXY001QUNLaGN5MTlUaEIKby9HMWlhN1ByOVUralJ5aWY5ZjAyRG9sd091V2ZLLzdSVUlhOEhEbHBvQlVERDkwRTVQUGdJSy9sTXB4RytXbwpwQ2N5bTBpWk9UT0JzNDE5bEh3TzA4bXFja1JsZEg5WExmbmY2UThqWFR5Ym1yYzdWNmwyNVprcTl4U0owbHR1CndmTnVTSzNCZnFtNkQxOUY0aTVCbmVaSWhjRVJTYlFLWDFxbWNqYnZFL2cyQko4TzhaZUgrd0RzTHJiNnZXQVIKY3BYWG1uelE3Vlp6ZklHTGVLVU1CTTh6SW0rcXI4RGZ4SEhSeVU1QTE3cFU4cy9MNUp5RXE1RGJjc2Q2dHlnbQp5UE9BYUNzWldVREI3UGhLOHpUWU9WYi9MM1lnSTU4bjFXek5IM0s5cmFreUppTmUxTE9GVVZzQTFDUnhtQ2YzCmlRSURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K")
	// packed, err := operatorPublicKeyABI.Pack("publicKey", [][]byte{key})
	// if err != nil {
	// 	t.Fatal(err)
	// }

	// Generate test chain after a connection to the server.
	// While processing blocks the events will be emitted which is read by subscription
	const chainLength = 30
	for i := 0; i <= chainLength; i++ {
		// Emit event OperatorAdded
		tx, err := boundContract.SimcontractTransactor.RegisterOperator(auth, operatorPubKey.Serialize(), big.NewInt(100_000_000))
		require.NoError(t, err)
		sim.Commit()
		receipt, err := sim.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Errorf("get receipt: %v", err)
		}
		require.Equal(t, uint64(0x1), receipt.Status)
	}

	eb := eventbatcher.NewEventBatcher()
	edh := setupEventDataHandler(t, ctx, logger)
	eventDispatcher := New(
		client,
		eb,
		edh,
		WithLogger(logger),
	)

	lastProcessedBlock, err := eventDispatcher.SyncHistory(ctx, 0)
	require.NoError(t, err)
	require.NoError(t, client.Close())
	require.NoError(t, eventDispatcher.SyncOngoing(ctx, lastProcessedBlock+1))
}

func setupEventDataHandler(t *testing.T, ctx context.Context, logger *zap.Logger) *eventdatahandler.EventDataHandler {
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, true)
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

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	parser := eventparser.New(contractFilterer)

	edh, err := eventdatahandler.New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig.Domain,
		operatorData,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		eventdatahandler.WithFullNode(),
		eventdatahandler.WithLogger(logger))

	if err != nil {
		t.Fatal(err)
	}
	return edh
}

func simTestBackend(testAddr ethcommon.Address) *simulator.SimulatedBackend {
	return simulator.NewSimulatedBackend(
		core.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, 10000000,
	)
}

func setupOperatorStorage(logger *zap.Logger, db basedb.IDb) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}
	operatorPubKey, err := nodeStorage.SetupPrivateKey("", true)
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(nil, operatorPubKey)
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
