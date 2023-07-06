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
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
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
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr     = crypto.PubkeyToAddress(testKey.PublicKey)
	testBalance  = big.NewInt(2e18)
	contractAddr = ethcommon.HexToAddress("0x3A220f351252089D385b29beca14e27F204c296A")
)

func TestEventDispatcher(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 10000 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	blockStream := make(chan []*types.Block)
	defer close(blockStream)
	done := make(chan interface{})
	defer close(done)

	// Create sim instance with a delay between block execution
	backend, processedStream := newTestBackend(t, done, blockStream, time.Microsecond*50)

	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	// Generate test chain after a connection to the server.
	// While processing blocks the events will be emited which is read by subscription
	generateInitialTestChain(t, done, blockStream, 1000)
	for blocks := range processedStream {
		t.Log("Processed blocks: ", len(blocks))
	}

	// Check if the contract is deployes successfuly with a standard eth1 client
	ec, err := backend.Attach()
	require.NoError(t, err)
	cl := ethclient.NewClient(ec)
	receipt, err := cl.TransactionReceipt(ctx, ethcommon.HexToHash("0x348887eab2e1c27dcce22b81482be5ad0e0dbc6fa2f7bea314ec84c819aa0d29"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), receipt.Status)
	contracctCode, err := cl.CodeAt(ctx, receipt.ContractAddress, nil)
	require.NoError(t, err)
	if len(contracctCode) == 0 {
		t.Fatal("got code for account that does not have contract code")
	}

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")
	client := executionclient.New(addr, contractAddr, executionclient.WithLogger(logger))
	client.Connect(ctx)

	_, err = client.IsReady(ctx)
	if err != nil {
		t.Fatal(err)
	}

	eb := eventbatcher.NewEventBatcher()
	ehd := NewEventDataHandler(t, ctx, logger)
	eventDispatcher := New(
		client,
		eb,
		ehd,
		WithLogger(logger),
	)

	err = eventDispatcher.Start(ctx, 0)
	require.NoError(t, err)
}

func NewEventDataHandler(t *testing.T, ctx context.Context, logger *zap.Logger) *eventdatahandler.EventDataHandler {
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := eventdb.NewEventDB(db.Badger())
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
	cl := executionclient.New("test", ethcommon.Address{})
	edh, err := eventdatahandler.New(
		eventDB,
		cl,
		validatorCtrl,
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
				Gas:      1000000,
				Data:     ethcommon.FromHex(callableBin),
			})
			g.AddTx(tx)
			t.Log("Tx hash", tx.Hash().Hex())
		} else {
			// Transactions to contract
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				To:       &contractAddr,
				Nonce:    uint64(i - 1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      1000000,
				// Call to function which emits event
				Data: ethcommon.FromHex("0x2acde098"),
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

/*
Example contract to test event emission:

	pragma solidity >=0.7.0 <0.9.0;
	contract SSVTest {
		event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee);
		function registerOperator() public { emit OperatorAdded(1, address(0), '0xabcd', 1000); }
	}
*/

const callableBin = "0x608060405234801561001057600080fd5b5061019f806100206000396000f3fe608060405234801561001057600080fd5b506004361061002b5760003560e01c80632acde09814610030575b600080fd5b61003861003a565b005b600073ffffffffffffffffffffffffffffffffffffffff1660017fd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f46103e8604051610085919061013b565b60405180910390a3565b600082825260208201905092915050565b7f3078616263640000000000000000000000000000000000000000000000000000600082015250565b60006100d660068361008f565b91506100e1826100a0565b602082019050919050565b6000819050919050565b6000819050919050565b6000819050919050565b600061012561012061011b846100ec565b610100565b6100f6565b9050919050565b6101358161010a565b82525050565b60006040820190508181036000830152610154816100c9565b9050610163602083018461012c565b9291505056fea26469706673582212209166a516a1bda4d10d473e246b349a414899539a15f0bf0188024af020ff265064736f6c63430008120033"
