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

	contractAddr = ethcommon.HexToAddress("0xdB7d6AB1f17c6b31909aE466702703dAEf9269Cf")
)

var genesis = &core.Genesis{
	Config:    params.AllEthashProtocolChanges,
	Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFee),
}

var testTx1 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    0,
	Value:    big.NewInt(12),
	GasPrice: big.NewInt(params.InitialBaseFee),
	Gas:      params.TxGas,
	To:       &ethcommon.Address{2},
})

var testTx2 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    1,
	Value:    big.NewInt(0),
	GasPrice: big.NewInt(params.InitialBaseFee),
	Gas:      100000,
	Data:     ethcommon.FromHex(abiBin),
})

var testTx3 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    2,
	Value:    big.NewInt(0),
	GasPrice: big.NewInt(params.InitialBaseFee),
	Gas:      100000,
	To:       &contractAddr,
	Data:     ethcommon.FromHex("0xa69b6ed0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000010100000000000000000000000000000000000000000000000000000000000000"),
})

// the following is based on this contract:
//
//	 contract T {
//	 	event received(address sender, uint amount, bytes memo);
//	 	event receivedAddr(address sender);
//
//	 	function receive(bytes calldata memo) external payable returns (string memory res) {
//	 		emit received(msg.sender, msg.value, memo);
//	 		emit receivedAddr(msg.sender);
//			return "hello world";
//	 	}
//	 }
const abiJSON = `[ { "constant": false, "inputs": [ { "name": "memo", "type": "bytes" } ], "name": "receive", "outputs": [ { "name": "res", "type": "string" } ], "payable": true, "stateMutability": "payable", "type": "function" }, { "anonymous": false, "inputs": [ { "indexed": false, "name": "sender", "type": "address" }, { "indexed": false, "name": "amount", "type": "uint256" }, { "indexed": false, "name": "memo", "type": "bytes" } ], "name": "received", "type": "event" }, { "anonymous": false, "inputs": [ { "indexed": false, "name": "sender", "type": "address" } ], "name": "receivedAddr", "type": "event" } ]`
const abiBin = `0x608060405234801561001057600080fd5b506102a0806100206000396000f3fe60806040526004361061003b576000357c010000000000000000000000000000000000000000000000000000000090048063a69b6ed014610040575b600080fd5b6100b76004803603602081101561005657600080fd5b810190808035906020019064010000000081111561007357600080fd5b82018360208201111561008557600080fd5b803590602001918460018302840111640100000000831117156100a757600080fd5b9091929391929390505050610132565b6040518080602001828103825283818151815260200191508051906020019080838360005b838110156100f75780820151818401526020810190506100dc565b50505050905090810190601f1680156101245780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b60607f75fd880d39c1daf53b6547ab6cb59451fc6452d27caa90e5b6649dd8293b9eed33348585604051808573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001848152602001806020018281038252848482818152602001925080828437600081840152601f19601f8201169050808301925050509550505050505060405180910390a17f46923992397eac56cf13058aced2a1871933622717e27b24eabc13bf9dd329c833604051808273ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200191505060405180910390a16040805190810160405280600b81526020017f68656c6c6f20776f726c6400000000000000000000000000000000000000000081525090509291505056fea165627a7a72305820ff0c57dad254cfeda48c9cfb47f1353a558bccb4d1bc31da1dae69315772d29e0029`
const deployedCode = `60806040526004361061003b576000357c010000000000000000000000000000000000000000000000000000000090048063a69b6ed014610040575b600080fd5b6100b76004803603602081101561005657600080fd5b810190808035906020019064010000000081111561007357600080fd5b82018360208201111561008557600080fd5b803590602001918460018302840111640100000000831117156100a757600080fd5b9091929391929390505050610132565b6040518080602001828103825283818151815260200191508051906020019080838360005b838110156100f75780820151818401526020810190506100dc565b50505050905090810190601f1680156101245780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b60607f75fd880d39c1daf53b6547ab6cb59451fc6452d27caa90e5b6649dd8293b9eed33348585604051808573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001848152602001806020018281038252848482818152602001925080828437600081840152601f19601f8201169050808301925050509550505050505060405180910390a17f46923992397eac56cf13058aced2a1871933622717e27b24eabc13bf9dd329c833604051808273ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200191505060405180910390a16040805190810160405280600b81526020017f68656c6c6f20776f726c6400000000000000000000000000000000000000000081525090509291505056fea165627a7a72305820ff0c57dad254cfeda48c9cfb47f1353a558bccb4d1bc31da1dae69315772d29e0029`

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
	// contractAddr := ethcommon.HexToAddress(networkconfig.TestNetwork.RegistryContractAddr)
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
	blocks := generateTestChain()

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

func generateTestChain() []*types.Block {
	generate := func(i int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
		if i == 1 {
			g.AddTx(testTx1)
			g.AddTx(testTx2)
			g.AddTx(testTx3)
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
