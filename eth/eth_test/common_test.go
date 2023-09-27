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
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/golang/mock/gomock"
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

type TestEnv struct {
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
	validatorCtrl *mocks.MockController
	mockCtrl      *gomock.Controller
}

func (e *TestEnv) shutdown() {
	if e.mockCtrl != nil {
		e.mockCtrl.Finish()
	}

	if e.httpSrv != nil {
		e.httpSrv.Close()
	}

	if e.execClient != nil {
		// Always returns nil error
		_ = e.execClient.Close()
	}
}

func (e *TestEnv) setup(
	t *testing.T,
	ctx context.Context,
	testAddresses []*ethcommon.Address,
	validatorsCount uint64,
	operatorsCount uint64,
) error {
	logger := zaptest.NewLogger(t)

	// Create operators RSA keys
	ops, err := createOperators(operatorsCount, 0)
	if err != nil {
		return err
	}

	validators := make([]*testValidatorData, validatorsCount)
	shares := make([][]byte, validatorsCount)

	// Create validators, BLS keys, shares
	for i := 0; i < int(validatorsCount); i++ {
		validators[i], err = createNewValidator(ops)
		if err != nil {
			return err
		}

		shares[i], err = generateSharesData(validators[i], ops, testAddrAlice, i)
		if err != nil {
			return err
		}
	}

	eh, validatorCtrl, mockCtrl, nodeStorage, err := setupEventHandler(t, ctx, logger, ops[0], &testAddrAlice, true)
	e.mockCtrl = mockCtrl
	e.nodeStorage = nodeStorage

	if err != nil {
		return err
	}
	if validatorCtrl == nil {
		return fmt.Errorf("error: validatorCtrl is empty")
	}

	// Adding testAddresses to the genesis block mostly to specify some balances for them
	sim := simTestBackend(testAddresses)

	// Create JSON-RPC handler
	rpcServer, err := sim.Node.RPCHandler()
	e.rpcServer = rpcServer
	if err != nil {
		return fmt.Errorf("creatingt rpc server: %w", err)
	}
	// Expose handler on a test server with ws open
	httpSrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	e.httpSrv = httpSrv

	addr := "ws:" + strings.TrimPrefix(httpSrv.URL, "http:")

	parsed, err := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	if err != nil {
		return fmt.Errorf("parsing contract abi: %w", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(testKeyAlice, big.NewInt(1337))
	if err != nil {
		return err
	}

	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim)
	if err != nil {
		return fmt.Errorf("deploying contract: %w", err)
	}

	sim.Commit()

	// Check contract code at the simulated blockchain
	contractCode, err := sim.CodeAt(ctx, contractAddr, nil)
	if err != nil {
		return fmt.Errorf("getting contract code: %w", err)
	}
	if len(contractCode) == 0 {
		return fmt.Errorf("error: contractCode is empty")
	}

	// Create a client and connect to the simulator
	e.execClient, err = executionclient.New(
		ctx,
		addr,
		contractAddr,
		executionclient.WithLogger(logger),
		executionclient.WithFollowDistance(0),
	)
	if err != nil {
		return err
	}

	err = e.execClient.Healthy(ctx)
	if err != nil {
		return err
	}

	e.boundContract, err = simcontract.NewSimcontract(contractAddr, sim)
	if err != nil {
		return err
	}

	metricsReporter := metricsreporter.New(
		metricsreporter.WithLogger(logger),
	)

	e.eventSyncer = eventsyncer.New(
		nodeStorage,
		e.execClient,
		eh,
		eventsyncer.WithLogger(logger),
		eventsyncer.WithMetrics(metricsReporter),
	)

	e.validatorCtrl = validatorCtrl
	e.sim = sim
	e.auth = auth
	e.validators = validators
	e.ops = ops
	e.shares = shares

	return nil
}

func commitBlock(sim *simulator.SimulatedBackend, blockNum *uint64) {
	sim.Commit()
	*blockNum++
}
