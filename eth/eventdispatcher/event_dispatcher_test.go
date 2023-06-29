package eventdispatcher

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdatahandler"
	"github.com/bloxapp/ssv/eth/eventdb"
	"github.com/bloxapp/ssv/eth/executionclient"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)
var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
	testBalance = big.NewInt(2e18)
	contractAddr = ethcommon.HexToAddress("0x3A220f351252089D385b29beca14e27F204c296A")
)


func TestEventDispatcher(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 1000 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	blockStream := make(chan []*types.Block)
	defer close(blockStream)
	done := make(chan interface{})
	defer close(done)

	eth1client,  processedStream := NewEth1Client(t, ctx, done, blockStream)
	eb := eventbatcher.NewEventBatcher()
	ehd := NewEventDataHandler(t, ctx, logger)
	eventDispatcher := New(
		eth1client,
		eb,
		ehd,
		WithLogger(logger),
	)

	err := eventDispatcher.Start(ctx, 0)
	require.NoError(t, err)
	// Generate test chain after a connection to the server.
	// While processing blocks the events will be emited which is read by subscription
	generateInitialTestChain(t, done, blockStream, 1000)
	for blocks := range processedStream {
		t.Log("Processed blocks: ", len(blocks))
	}

}

func NewEventDataHandler(t *testing.T,  ctx context.Context, logger *zap.Logger) *eventdatahandler.EventDataHandler {
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	if err != nil {
		t.Fatal(err)
	}
	
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

	edh := eventdatahandler.New(
		eventDB,
		validatorCtrl,
		operatorData,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		eventdatahandler.WithFullNode(),
		eventdatahandler.WithLogger(logger),
	)
	return edh
}

func NewEth1Client(t *testing.T, ctx context.Context, done chan interface{},blockStream chan []*types.Block, ) (*executionclient.ExecutionClient, <-chan []*types.Block){
	// Create sim instance with a delay between block execution
	backend, processedStream := newTestBackend(t, done, blockStream, time.Microsecond * 50)
	
	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")
	logger := zaptest.NewLogger(t)
	client := executionclient.New(addr, contractAddr, executionclient.WithLogger(logger))
	client.Connect(ctx)
	_, err := client.IsReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return client, processedStream
}



var genesis = &core.Genesis{
	Config:    params.AllEthashProtocolChanges,
	Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFee),
}

func newTestBackend(t *testing.T, done <-chan interface{}, blockStream <-chan []*types.Block, delay time.Duration) (*node.Node, <-chan []*types.Block) {
	processedStream := make(chan []*types.Block)
	// Create node
	n, err := node.New(&node.Config{})

	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}
	// Create Ethereum Service
	config := &ethconfig.Config{Genesis: genesis}
	ethservice, err := eth.New(n, config)
	if err != nil {
		t.Fatalf("can't create new ethereum service: %v", err)
	}

	// Add required APIs
	filterSystem := filters.NewFilterSystem(ethservice.APIBackend, filters.Config{})
	n.RegisterAPIs([]rpc.API{{
		Namespace: "eth",
		Service:   filters.NewFilterAPI(filterSystem, false),
	}})

	// Start eth1 node
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}

	go func() {
		defer close(processedStream)
		select {
		case <-done:
			return
		case blocks := <-blockStream:
			// Batch block processing because events are fired only after all blocks in the batch processed
			// for _, block := range blocks {
			// 	var blocksToProcess []*types.Block
			// 	blocksToProcess = append(blocksToProcess, block)
			// 	if _, err := ethservice.BlockChain().InsertChain(blocksToProcess); err != nil {
			// 		return
			// 	}
			// 	processedStream <- blocksToProcess
			// 	time.Sleep(delay)
			// }
			if _, err := ethservice.BlockChain().InsertChain(blocks); err != nil {
				return
			}
			processedStream <- blocks
		}
	}()
	return n, processedStream
}

// Generate blocks with transactions
func generateInitialTestChain(t *testing.T, done <-chan interface{}, blockStream chan []*types.Block, n int) {
	generate := func(i int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
		if i == 0 {
			return
		}
		// Add contract deployment to the firs block
		if i == 1 {
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				Nonce:    uint64(i - 1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      100000,
				Data:     ethcommon.FromHex(callableBin),
			})
			g.AddTx(tx)
		} else {
			// Transactions to Callable contract
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				To:       &contractAddr,
				Nonce:    uint64(i - 1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      100000,
				// Call to function Call() which emits event Called()
				Data:     ethcommon.FromHex("0x34e22921"),
			})
			g.AddTx(tx)
		}
	}
	_, blocks, _ := core.GenerateChainWithGenesis(genesis, ethash.NewFaker(), n, generate)
	go func() {
		select {
		case <-done:
		case blockStream <- blocks:
		}
	}()
}

/*
Example contract to test event emission:

	pragma solidity >=0.7.0 <0.9.0;
	contract Callable {
		event Called();
		function Call() public { emit Called(); }
	}
*/
const callableAbi = "[{\"anonymous\":false,\"inputs\":[],\"name\":\"Called\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"Call\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"

const callableBin = "6080604052348015600f57600080fd5b5060998061001e6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c806334e2292114602d575b600080fd5b60336035565b005b7f81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b860405160405180910390a156fea2646970667358221220029436d24f3ac598ceca41d4d712e13ced6d70727f4cdc580667de66d2f51d8b64736f6c63430008010033"


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
