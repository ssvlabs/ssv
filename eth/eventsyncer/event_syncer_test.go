package eventsyncer

import (
	"context"
	"encoding/base64"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/ekm"
	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/eventhandler"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/simulator"
	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/keys"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/rsaencryption"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestEventSyncer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	blockStream := make(chan []*ethtypes.Block)
	defer close(blockStream)
	done := make(chan struct{})
	defer close(done)

	// Create sim instance with a delay between block execution
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node().RPCHandler()
	httpSrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpSrv.Close()

	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim.Client())
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Check contract code at the simulated blockchain
	contractCode, err := sim.Client().CodeAt(ctx, contractAddr, nil)
	if err != nil {
		t.Errorf("getting contract code: %v", err)
	}
	require.NotEmpty(t, contractCode)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim.Client())
	require.NoError(t, err)

	addr := "ws:" + strings.TrimPrefix(httpSrv.URL, "http:")
	client, err := executionclient.New(ctx, addr, contractAddr, executionclient.WithLogger(logger))
	require.NoError(t, err)

	err = client.Healthy(ctx)
	require.NoError(t, err)

	// Generate operator key
	opPubKey, _, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)

	pkstr := base64.StdEncoding.EncodeToString(opPubKey)
	pckd, err := eventparser.PackOperatorPublicKey([]byte(pkstr))
	require.NoError(t, err)

	// Generate test chain after a connection to the server.
	// While processing blocks the events will be emitted which is read by subscription
	const chainLength = 30
	for i := 0; i <= chainLength; i++ {
		// Emit event OperatorAdded
		tx, err := boundContract.SimcontractTransactor.RegisterOperator(auth, pckd, big.NewInt(100_000_000))
		require.NoError(t, err)
		sim.Commit()
		receipt, err := sim.Client().TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Errorf("get receipt: %v", err)
		}
		require.Equal(t, uint64(0x1), receipt.Status)
	}
	db, err := kv.NewInMemory(logger, basedb.Options{
		Ctx: ctx,
	})
	require.NoError(t, err)
	privateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)
	nodeStorage, operatorData := setupOperatorStorage(logger, db, privateKey)
	require.NoError(t, err)

	eh := setupEventHandler(t, ctx, logger, db, nodeStorage, operatorData, privateKey)
	eventSyncer := New(
		nodeStorage,
		client,
		eh,
		WithLogger(logger),
		WithStalenessThreshold(time.Second*10),
	)

	nodeStorage.SaveLastProcessedBlock(nil, big.NewInt(1))
	err = eventSyncer.Healthy(ctx)
	require.NoError(t, err)

	lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, 0)
	require.NoError(t, err)
	require.NoError(t, client.Close())
	require.NoError(t, eventSyncer.SyncOngoing(ctx, lastProcessedBlock+1))
}

func setupEventHandler(
	t *testing.T,
	ctx context.Context,
	logger *zap.Logger,
	db *kv.BadgerDB,
	nodeStorage operatorstorage.Storage,
	operatorData *registrystorage.OperatorData,
	privateKey keys.OperatorPrivateKey,
) *eventhandler.EventHandler {
	operatorDataStore := operatordatastore.New(operatorData)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, "")
	if err != nil {
		logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bc := beacon.NewMockBeaconNode(ctrl)
	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{
		Context:           ctx,
		NetworkConfig:     testNetworkConfig,
		DB:                db,
		RegistryStorage:   nodeStorage,
		OperatorDataStore: operatorDataStore,
		ValidatorsMap:     validators.New(ctx),
	})

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	parser := eventparser.New(contractFilterer)

	eh, err := eventhandler.New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig,
		operatorDataStore,
		privateKey,
		keyManager,
		bc,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger))

	if err != nil {
		t.Fatal(err)
	}
	return eh
}

func simTestBackend(testAddr ethcommon.Address) *simulator.Backend {
	return simulator.NewBackend(
		types.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, simulated.WithBlockGasLimit(10000000),
	)
}

func setupOperatorStorage(logger *zap.Logger, db basedb.Database, privKey keys.OperatorPrivateKey) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	privKeyHash, err := privKey.StorageHash()
	if err != nil {
		logger.Fatal("failed to hash operator private key", zap.Error(err))
	}

	encodedPubKey, err := privKey.Public().Base64()
	if err != nil {
		logger.Fatal("failed to encode operator public key", zap.Error(err))
	}

	if err := nodeStorage.SavePrivateKeyHash(privKeyHash); err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKeyHash()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(nil, encodedPubKey)
	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: encodedPubKey,
		}
	}

	return nodeStorage, operatorData
}
