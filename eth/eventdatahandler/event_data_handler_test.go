package eventdatahandler

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/simulator"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/blskeygen"
	"github.com/bloxapp/ssv/utils/threshold"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestHandleBlockEventsStream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eb := eventbatcher.NewEventBatcher()
	edh, err := setupDataHandler(t, ctx, logger)
	if err != nil {
		t.Fatal(err)
	}
	sim := simTestBackend(testAddr)

	// Create JSON-RPC handler
	rpcServer, _ := sim.Node.RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

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

	// Create a client and connect to the simulator
	client := executionclient.New(addr, contractAddr, executionclient.WithLogger(logger), executionclient.WithFinalizationOffset(0))
	client.Connect(ctx)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)

	logs := client.StreamLogs(ctx, 0)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim)
	require.NoError(t, err)

	// Generate operator key
	_, operatorPubKey := blskeygen.GenBLSKeyPair()

	// Generate validator shares
	validatorShares, _, err := newValidator()
	require.NoError(t, err)
	// Encode shares
	encodedValidatorShares, err := validatorShares.Encode()
	require.NoError(t, err)
	t.Run("test OperatorAdded event handle", func(t *testing.T) {
		// Call the contract method
		_, err := boundContract.SimcontractTransactor.RegisterOperator(auth, operatorPubKey.Serialize(), big.NewInt(100_000_000))
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		// Check that there is no registered operators
		operators, err := edh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 0, len(operators))

		// Hanlde the event
		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x2), lastProcessedBlock)
		require.NoError(t, err)

		// Check storage for a new operator
		operators, err = edh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(operators))

		// Check if an operator in the storage has same attributes
		operatorAddedEvent, err := edh.eventFilterer.ParseOperatorAdded(log)
		require.NoError(t, err)
		data, _, err := edh.nodeStorage.GetOperatorData(nil, operatorAddedEvent.OperatorId)
		require.NoError(t, err)
		require.Equal(t, operatorAddedEvent.OperatorId, data.ID)
		require.Equal(t, operatorAddedEvent.Owner, data.OwnerAddress)
		require.Equal(t, operatorPubKey.Serialize(), data.PublicKey)
	})
	t.Run("test OperatorRemoved event handle", func(t *testing.T) {
		// Call the contract method
		_, err = boundContract.SimcontractTransactor.RemoveOperator(auth, 1)
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		// Check that there is 1 registered operator
		operators, err := edh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(operators))

		// Hanlde the event
		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x3), lastProcessedBlock)
		require.NoError(t, err)

		// Check if the operator was removed successfuly
		// TODO: this should be adjusted when eth/eventdatahandler/handlers.go#L109 is resolved
		operators, err = edh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(operators))
	})

	t.Run("test ValidatorAdded event handle", func(t *testing.T) {
		// Call the contract method
		_, err = boundContract.SimcontractTransactor.RegisterValidator(
			auth,
			validatorShares.ValidatorPubKey,
			[]uint64{1, 2, 3, 4},
			encodedValidatorShares,
			big.NewInt(100_000_000),
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           1,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x4), lastProcessedBlock)
		require.NoError(t, err)
	})

	t.Run("test ValidatorRemoved event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.RemoveValidator(
			auth,
			validatorShares.ValidatorPubKey,
			[]uint64{1, 2, 3, 4},
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           1,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x5), lastProcessedBlock)
		require.NoError(t, err)
	})

	t.Run("test ClusterLiquidated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.Liquidate(
			auth,
			ethcommon.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"),
			[]uint64{1, 2, 3, 4},
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           1,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x6), lastProcessedBlock)
		require.NoError(t, err)
	})

	t.Run("test ClusterReactivated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.Reactivate(
			auth,
			[]uint64{1, 2, 3},
			big.NewInt(100_000_000),
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           1,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x7), lastProcessedBlock)
		require.NoError(t, err)
	})

	t.Run("test FeeRecipientAddressUpdated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.SetFeeRecipientAddress(
			auth,
			ethcommon.HexToAddress("0x1"),
		)
		require.NoError(t, err)
		sim.Commit()

		log := <-logs
		require.Equal(t, ethcommon.HexToHash("0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548"), log.Topics[0])

		eventsCh := make(chan ethtypes.Log)
		go func() {
			defer close(eventsCh)
			eventsCh <- log
		}()

		lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), false)
		require.Equal(t, uint64(0x8), lastProcessedBlock)
		require.NoError(t, err)
		// Check if the fee recepient was updated
		recepientData, _, err := edh.nodeStorage.GetRecipientData(nil, ethcommon.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"))
		require.NoError(t, err)
		require.Equal(t, ethcommon.HexToAddress("0x1").String(), recepientData.FeeRecipient.String())
	})
}

func setupDataHandler(t *testing.T, ctx context.Context, logger *zap.Logger) (*EventDataHandler, error) {
	options := basedb.Options{
		Type:       "badger-memory",
		Path:       "",
		Reporting:  false,
		GCInterval: 0,
		Ctx:        ctx,
	}

	db, err := ssvstorage.GetStorageFactory(logger, options)

	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, true)
	if err != nil {
		return nil, err
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bc := beacon.NewMockBeaconNode(ctrl)
	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{
		Context:         ctx,
		DB:              db,
		RegistryStorage: nodeStorage,
		KeyManager:      keyManager,
		StorageMap:      storageMap,
	})

	cl := executionclient.New("test", ethcommon.Address{})

	contractFilterer, err := cl.Filterer()
	require.NoError(t, err)

	contractABI, err := contract.ContractMetaData.GetAbi()
	require.NoError(t, err)

	edh, err := New(
		nodeStorage,
		contractFilterer,
		contractABI,
		validatorCtrl,
		testNetworkConfig.Domain,
		operatorData,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		WithFullNode(),
		WithLogger(logger))
	if err != nil {
		return nil, err
	}
	return edh, nil
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

func unmarshalLog(t *testing.T, rawOperatorAdded string) ethtypes.Log {
	var vLogOperatorAdded ethtypes.Log
	err := json.Unmarshal([]byte(rawOperatorAdded), &vLogOperatorAdded)
	require.NoError(t, err)
	contractAbi, err := abi.JSON(strings.NewReader(contract.ContractMetaData.ABI))
	require.NoError(t, err)
	require.NotNil(t, contractAbi)
	return vLogOperatorAdded
}

func shareWithPK(pk string) *ssvtypes.SSVShare {
	return &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: ethcommon.FromHex(pk)}}
}

var mockStartTask1 = &StartValidatorTask{share: shareWithPK("0x1")}
var mockStartTask2 = &StartValidatorTask{share: shareWithPK("0x2")}
var mockStopTask1 = &StopValidatorTask{publicKey: ethcommon.FromHex("0x1")}
var mockStopTask2 = &StopValidatorTask{publicKey: ethcommon.FromHex("0x2")}
var mockLiquidateTask1 = &LiquidateClusterTask{toLiquidate: []*ssvtypes.SSVShare{shareWithPK("0x1"), shareWithPK("0x2")}}
var mockLiquidateTask2 = &LiquidateClusterTask{toLiquidate: []*ssvtypes.SSVShare{shareWithPK("0x3"), shareWithPK("0x4")}}
var mockReactivateTask1 = &ReactivateClusterTask{toReactivate: []*ssvtypes.SSVShare{shareWithPK("0x1"), shareWithPK("0x2")}}
var mockReactivateTask2 = &ReactivateClusterTask{toReactivate: []*ssvtypes.SSVShare{shareWithPK("0x3"), shareWithPK("0x4")}}
var mockUpdateFeeTask1 = &UpdateFeeRecipientTask{owner: ethcommon.HexToAddress("0x1"), recipient: ethcommon.HexToAddress("0x1")}
var mockUpdateFeeTask2 = &UpdateFeeRecipientTask{owner: ethcommon.HexToAddress("0x2"), recipient: ethcommon.HexToAddress("0x2")}
var mockUpdateFeeTask3 = &UpdateFeeRecipientTask{owner: ethcommon.HexToAddress("0x2"), recipient: ethcommon.HexToAddress("0x3")}

func TestFilterSupersedingTasks_SingleStartTask(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStartTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Single StartTask: expected the result length to be 1")
	require.Equal(t, mockStartTask1, result[0], "Single StartTask: expected the task to be equal to mockStartTask1")
}

func TestFilterSupersedingTasks_SingleStopTask(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStopTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Single StopTask: expected the result length to be 1")
	require.Equal(t, mockStopTask1, result[0], "Single StopTask: expected the task to be equal to mockStopTask1")
}

func TestFilterSupersedingTasks_StartAndStopTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStartTask1, mockStopTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 0, len(result), "Start and Stop tasks: expected the result length to be 0")
}

func TestFilterSupersedingTasks_SingleLiquidateTask(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockLiquidateTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Single LiquidateTask: expected the result length to be 1")
	require.Equal(t, mockLiquidateTask1, result[0], "Single LiquidateTask: expected the task to be equal to mockLiquidateTask1")
}

func TestFilterSupersedingTasks_SingleReactivateTask(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockReactivateTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Single ReactivateTask: expected the result length to be 1")
	require.Equal(t, mockReactivateTask1, result[0], "Single ReactivateTask: expected the task to be equal to mockReactivateTask1")
}

func TestFilterSupersedingTasks_LiquidateAndReactivateTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockLiquidateTask1, mockReactivateTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 0, len(result), "Liquidate and Reactivate tasks: expected the result length to be 0")
}

func TestFilterSupersedingTasks_SingleUpdateFeeTask(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockUpdateFeeTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Single UpdateFeeTask: expected the result length to be 1")
	require.Equal(t, mockUpdateFeeTask1, result[0], "Single UpdateFeeTask: expected the task to be equal to mockUpdateFeeTask1")
}

func TestFilterSupersedingTasks_MultipleUpdateFeeTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockUpdateFeeTask2, mockUpdateFeeTask3}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Multiple UpdateFeeTasks: expected the result length to be 1")
	require.Equal(t, mockUpdateFeeTask3, result[0], "Multiple UpdateFeeTasks: expected the task to be equal to mockUpdateFeeTask3")
}

func TestFilterSupersedingTasks_MixedTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStartTask1, mockStartTask2, mockStopTask1, mockLiquidateTask2, mockReactivateTask1, mockUpdateFeeTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 4, len(result), "Mixed tasks: expected the result length to be 3")
	require.Contains(t, result, mockStartTask2, "Mixed tasks: expected the tasks to contain mockStartTask2")
	require.Contains(t, result, mockLiquidateTask2, "Mixed tasks: expected the tasks to contain mockLiquidateTask2")
	require.Contains(t, result, mockReactivateTask1, "Mixed tasks: expected the tasks to contain mockReactivateTask1")
	require.Contains(t, result, mockUpdateFeeTask1, "Mixed tasks: expected the tasks to contain mockUpdateFeeTask1")
}

func TestFilterSupersedingTasks_MultipleDifferentStopTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStopTask1, mockStopTask2}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 2, len(result), "Multiple different StopTasks: expected the result length to be 2")
	require.Contains(t, result, mockStopTask1, "Multiple different StopTasks: expected the tasks to contain mockStopTask1")
	require.Contains(t, result, mockStopTask2, "Multiple different StopTasks: expected the tasks to contain mockStopTask2")
}

func TestFilterSupersedingTasks_MultipleDifferentReactivateTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockReactivateTask1, mockReactivateTask2}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 2, len(result), "Multiple different ReactivateTasks: expected the result length to be 2")
	require.Contains(t, result, mockReactivateTask1, "Multiple different ReactivateTasks: expected the tasks to contain mockReactivateTask1")
	require.Contains(t, result, mockReactivateTask2, "Multiple different ReactivateTasks: expected the tasks to contain mockReactivateTask2")
}

func TestFilterSupersedingTasks_MultipleDifferentUpdateFeeTasks(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockUpdateFeeTask1, mockUpdateFeeTask2}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 2, len(result), "Multiple different UpdateFeeTasks: expected the result length to be 2")
	require.Contains(t, result, mockUpdateFeeTask1, "Multiple different UpdateFeeTasks: expected the tasks to contain mockUpdateFeeTask1")
	require.Contains(t, result, mockUpdateFeeTask2, "Multiple different UpdateFeeTasks: expected the tasks to contain mockUpdateFeeTask2")
}

func TestFilterSupersedingTasks_MixedTasks_Different(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStartTask1, mockStopTask2, mockLiquidateTask1, mockReactivateTask2, mockUpdateFeeTask1}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 5, len(result), "Mixed different tasks: expected the result length to be 5")
	require.Contains(t, result, mockStartTask1, "Mixed different tasks: expected the tasks to contain mockStartTask1")
	require.Contains(t, result, mockStopTask2, "Mixed different tasks: expected the tasks to contain mockStopTask2")
	require.Contains(t, result, mockLiquidateTask1, "Mixed different tasks: expected the tasks to contain mockLiquidateTask1")
	require.Contains(t, result, mockReactivateTask2, "Mixed different tasks: expected the tasks to contain mockReactivateTask2")
	require.Contains(t, result, mockUpdateFeeTask1, "Mixed different tasks: expected the tasks to contain mockUpdateFeeTask1")
}

type unknownTask struct{}

func (u unknownTask) Execute() error { return nil }

func TestFilterSupersedingTasks_UnknownTask(t *testing.T) {
	edh := &EventDataHandler{}
	WithLogger(zaptest.NewLogger(t))(edh)
	WithTaskOptimizer()(edh)

	tasks := []Task{mockStartTask1, unknownTask{}}
	result := edh.filterSupersedingTasks(tasks)

	require.Equal(t, 1, len(result), "Unknown task: expected the result length to be 1")
	require.Contains(t, result, mockStartTask1, "Unknown task: expected the tasks to contain mockStartTask1")
}

func simTestBackend(testAddr ethcommon.Address) *simulator.SimulatedBackend {
	return simulator.NewSimulatedBackend(
		core.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, 10000000,
	)
}

func newValidator() (*ssvtypes.SSVShare, *bls.SecretKey, error) {
	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)

	validatorShare, _ := generateRandomValidatorShare(splitKeys)

	return validatorShare, sk, err
}

func generateRandomValidatorShare(splitKeys map[uint64]*bls.SecretKey) (*ssvtypes.SSVShare, *bls.SecretKey) {
	threshold.Init()

	sk1 := bls.SecretKey{}
	sk1.SetByCSPRNG()

	sk2 := bls.SecretKey{}
	sk2.SetByCSPRNG()

	ibftCommittee := []*spectypes.Operator{
		{
			OperatorID: 1,
			PubKey:     splitKeys[1].Serialize(),
		},
		{
			OperatorID: 2,
			PubKey:     splitKeys[2].Serialize(),
		},
		{
			OperatorID: 3,
			PubKey:     splitKeys[3].Serialize(),
		},
		{
			OperatorID: 4,
			PubKey:     splitKeys[4].Serialize(),
		},
	}

	return &ssvtypes.SSVShare{
		Share: spectypes.Share{
			OperatorID:      1,
			ValidatorPubKey: sk1.GetPublicKey().Serialize(),
			SharePubKey:     sk2.GetPublicKey().Serialize(),
			Committee:       ibftCommittee,
			Quorum:          3,
			PartialQuorum:   2,
			DomainType:      ssvtypes.GetDefaultDomain(),
			Graffiti:        nil,
		},
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beacon.ValidatorMetadata{
				Balance: 1,
				Status:  2,
				Index:   3,
			},
			OwnerAddress: ethcommon.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"),
			Liquidated:   true,
		},
	}, &sk1
}
