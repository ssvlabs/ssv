package eth_test

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/eth/eventsyncer"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/simulator"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap/zaptest"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
)

type CommonTestInput struct {
	t             *testing.T
	sim           *simulator.SimulatedBackend
	boundContract *simcontract.Simcontract
	blockNum      *uint64
	nodeStorage   storage.Storage
	doInOneBlock  bool
}

func NewCommonTestInput(
	t *testing.T,
	sim *simulator.SimulatedBackend,
	boundContract *simcontract.Simcontract,
	blockNum *uint64,
	nodeStorage storage.Storage,
	doInOneBlock bool,
) *CommonTestInput {
	return &CommonTestInput{
		t:             t,
		sim:           sim,
		boundContract: boundContract,
		blockNum:      blockNum,
		nodeStorage:   nodeStorage,
		doInOneBlock:  doInOneBlock,
	}
}

type testEnv struct {
	eventSyncer   *eventsyncer.EventSyncer
	validators    []*testValidatorData
	ops           []*testOperator
	nodeStorage   storage.Storage
	sim           *simulator.SimulatedBackend
	boundContract *simcontract.Simcontract
	auth          *bind.TransactOpts
	shares        [][]byte
	execClient    *executionclient.ExecutionClient
	rpcServer     *rpc.Server
	httpSrv       *httptest.Server
}

func setupEnv(
	t *testing.T,
	ctx context.Context,
	testAddresses []*ethcommon.Address,
	validatorsCount uint64,
	operatorsCount uint64,
) (*testEnv, error) {
	logger := zaptest.NewLogger(t)

	// Create operators RSA keys
	ops, err := createOperators(operatorsCount, 0)
	if err != nil {
		return nil, err
	}

	validators := make([]*testValidatorData, validatorsCount)
	shares := make([][]byte, 10)

	// Create validators, BLS keys, shares
	for i := 0; i < 10; i++ {
		validators[i], err = createNewValidator(ops)
		if err != nil {
			return nil, err
		}

		shares[i], err = generateSharesData(validators[i], ops, testAddrAlice, i)
		if err != nil {
			return nil, err
		}
	}

	eh, validatorCtrl, nodeStorage, err := setupEventHandler(t, ctx, logger, ops[0], &testAddrAlice, true)
	if err != nil {
		return nil, err
	}
	if validatorCtrl == nil {
		return nil, fmt.Errorf("error: validatorCtrl is empty")
	}

	// Adding testAddresses to the genesis block mostly to specify some balances for them
	sim := simTestBackend(testAddresses)

	// Create JSON-RPC handler
	rpcServer, _ := sim.Node.RPCHandler()
	// Expose handler on a test server with ws open
	httpSrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	addr := "ws:" + strings.TrimPrefix(httpSrv.URL, "http:")

	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKeyAlice, big.NewInt(1337))
	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
		return nil, err
	}
	sim.Commit()

	// Check contract code at the simulated blockchain
	contractCode, err := sim.CodeAt(ctx, contractAddr, nil)
	if err != nil {
		t.Errorf("getting contract code: %v", err)
	}
	if len(contractCode) == 0 {
		return nil, fmt.Errorf("error: contractCode is empty")
	}

	// Create a client and connect to the simulator
	client, err := executionclient.New(
		ctx,
		addr,
		contractAddr,
		executionclient.WithLogger(logger),
		executionclient.WithFollowDistance(0),
	)

	if err != nil {
		return nil, err
	}

	err = client.Healthy(ctx)
	if err != nil {
		return nil, err
	}

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim)
	if err != nil {
		return nil, err
	}

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

	return &testEnv{
		eventSyncer:   eventSyncer,
		boundContract: boundContract,
		sim:           sim,
		nodeStorage:   nodeStorage,
		auth:          auth,
		ops:           ops,
		validators:    validators,
		shares:        shares,
		execClient:    client,
		rpcServer:     rpcServer,
		httpSrv:       httpSrv,
	}, nil
}
