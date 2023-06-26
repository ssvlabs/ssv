package eth1client

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)

	testBalance = big.NewInt(2e18)

	contractAddr = ethcommon.HexToAddress("0x3A220f351252089D385b29beca14e27F204c296A")
)

var genesis = &core.Genesis{
	Config:    params.AllEthashProtocolChanges,
	Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFee),
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

func TestEth1Client(t *testing.T) {
	const testTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	backend, _ := newTestBackend(t)
	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	logger := zaptest.NewLogger(t)

	client := New(addr, contractAddr, WithLogger(logger))

	require.NoError(t, client.Connect(ctx))

	ready, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, ready)

	tests := map[string]struct {
		test func(t *testing.T)
	}{
		"Logs": {
			func(t *testing.T) { testFetchHistoricalLogs(t, client, addr) },
		},
	}

	t.Parallel()
	for name, tt := range tests {
		t.Run(name, tt.test)
	}

	require.NoError(t, client.Close())
	require.NoError(t, backend.Close())

}

func newTestBackend(t *testing.T) (*node.Node, []*types.Block) {
	// Generate test chain.
	blocks := generateTestChain(t)

	// Create node
	n, err := node.New(&node.Config{})

	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}
	// Create Ethereum Service
	config := &ethconfig.Config{Genesis: genesis}
	config.Ethash.PowMode = ethash.ModeFake
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

	// Import the test chain.
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}
	if _, err := ethservice.BlockChain().InsertChain(blocks[1:]); err != nil {
		t.Fatalf("can't import test blocks: %v", err)
	}
	return n, blocks
}

func generateTestChain(t *testing.T) []*types.Block {
	generate := func(i int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
		if i == 0 {
			return
		}
		if i == 1 {
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				Nonce:    uint64(i-1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      100000,
				Data: ethcommon.FromHex(callableBin),
			})
			g.AddTx(tx)
			t.Log("Tx hash", tx.Hash().Hex())
		} else {
			to := ethcommon.HexToAddress("0x3A220f351252089D385b29beca14e27F204c296A")
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				To: &to,
				Nonce:    uint64(i-1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      100000,
				Data: ethcommon.FromHex("0x34e22921"),
			})
			g.AddTx(tx)
			t.Log("Tx hash", tx.Hash().Hex())
		}
	}
	_, blocks, _ := core.GenerateChainWithGenesis(genesis, ethash.NewFaker(), 20, generate)

	return append([]*types.Block{genesis.ToBlock()}, blocks...)
}

func testFetchHistoricalLogs(t *testing.T, client *Eth1Client, addr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	logs, _, err := client.FetchHistoricalLogs(ctx, 0)
	if err != nil {
		t.Fatalf("FetchHistoricalLogs error = %q", err)
	}
	require.NoError(t, err)
	require.NotEmpty(t, logs)
}
