package eth_test

import (
	"context"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/eventsyncer"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
)

var (
	testKeyAlice, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testKeyBob, _   = crypto.HexToECDSA("42e14d227125f411d6d3285bb4a2e07c2dba2e210bd2f3f4e2a36633bd61bfe6")

	testAddrAlice = crypto.PubkeyToAddress(testKeyAlice.PublicKey)
	testAddrBob   = crypto.PubkeyToAddress(testKeyBob.PublicKey)
)

// E2E tests for ETH package!
func TestEthExecLayer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	operatorsCount := uint64(0)
	// Create operators rsa keys
	ops, err := createOperators(4, operatorsCount)
	require.NoError(t, err)

	eh, validatorCtrl, nodeStorage, err := setupEventHandler(t, ctx, logger, ops[0], &testAddrAlice, true)
	require.NoError(t, err)
	require.NotNil(t, validatorCtrl)

	testAddresses := make([]*ethcommon.Address, 2)
	testAddresses[0] = &testAddrAlice
	testAddresses[1] = &testAddrBob

	// Adding testAddresses to the genesis block mostly to specify some balances for them
	sim := simTestBackend(testAddresses)

	// Create JSON-RPC handler
	rpcServer, _ := sim.Node.RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKeyAlice, big.NewInt(1337))
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
	client, err := executionclient.New(
		ctx, addr, contractAddr, executionclient.WithLogger(logger), executionclient.WithFollowDistance(0))
	require.NoError(t, err)

	err = client.Healthy(ctx)
	require.NoError(t, err)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim)
	require.NoError(t, err)

	metricsReporter := metricsreporter.New(
		metricsreporter.WithLogger(logger),
	)

	eventSyncer := eventsyncer.New(
		nodeStorage,
		client,
		eh,
		eventsyncer.WithLogger(logger),
		eventsyncer.WithMetrics(metricsReporter),
	)

	lastHandledBlockNum := uint64(0x1)

	// Generate a new validator
	validatorData1, err := createNewValidator(ops)
	require.NoError(t, err)
	sharesData1, err := generateSharesData(validatorData1, ops, testAddrAlice, 0)
	require.NoError(t, err)

	// Create another validator. We'll create the shares later in the tests
	validatorData2, err := createNewValidator(ops)
	require.NoError(t, err)

	blockNum := uint64(0x1)

	// Prepare blocks with events
	// Check that the state is empty before the test
	// Check SyncHistory doesn't execute any tasks -> doesn't run any of Controller methods
	// Check the node storage for existing of operators and a validator
	t.Run("SyncHistory happy flow", func(t *testing.T) {
		//validatorCtrl.

		// BLOCK 2. produce OPERATOR ADDED
		// Check that there are no registered operators
		operators, err := nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 0, len(operators))

		for _, op := range ops {
			// Call the contract method
			packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op.pub)
			require.NoError(t, err)
			_, err = boundContract.SimcontractTransactor.RegisterOperator(auth, packedOperatorPubKey, big.NewInt(100_000_000))
			require.NoError(t, err)
		}
		sim.Commit()
		blockNum++

		// BLOCK 3:  VALIDATOR ADDED:
		// Check that there were no operations for Alice Validator
		nonce, err := nodeStorage.GetNextNonce(nil, testAddrAlice)
		require.NoError(t, err)
		require.Equal(t, registrystorage.Nonce(0), nonce)
		// Check there are no shares related to the Alice Validator
		valPubKey := validatorData1.masterPubKey.Serialize()
		aliceShares := nodeStorage.Shares().Get(nil, valPubKey)
		require.Nil(t, aliceShares)

		// Call the contract method
		_, err = boundContract.SimcontractTransactor.RegisterValidator(
			auth,
			validatorData1.masterPubKey.Serialize(),
			[]uint64{1, 2, 3, 4},
			sharesData1,
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
		blockNum++

		// Run SyncHistory
		lastHandledBlockNum, err = eventSyncer.SyncHistory(ctx, lastHandledBlockNum)
		require.NoError(t, err)

		//check all the events were handled correctly and block number was increased
		require.Equal(t, blockNum, lastHandledBlockNum)

		// Check that operators were successfully registered
		operators, err = nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, len(ops), len(operators))
		// Check that validator was registered
		shares := nodeStorage.Shares().List(nil)
		require.Equal(t, 1, len(shares))
		// Check the nonce was bumped
		nonce, err = nodeStorage.GetNextNonce(nil, testAddrAlice)
		require.NoError(t, err)
		require.Equal(t, registrystorage.Nonce(1), nonce)
	})

	// Main difference between "online" events handling and syncing the historical (old) events
	// is that here we have to check that the controller was triggered
	t.Run("SyncOngoing happy flow", func(t *testing.T) {

	})

	_ = validatorData2
}
