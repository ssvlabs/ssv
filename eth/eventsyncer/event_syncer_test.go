package eventsyncer

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/ekmadapter"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/eventhandler"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/simulator"
	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validators"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/threshold"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestEventSyncer(t *testing.T) {
	logger := zaptest.NewLogger(t)

	const testTimeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

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

	// Generate operator key
	opPubKey, _, err := rsaencryption.GenerateKeyPairPEM()
	require.NoError(t, err)

	pkstr := base64.StdEncoding.EncodeToString(opPubKey)
	pckd, err := eventparser.PackOperatorPublicKey(pkstr)
	require.NoError(t, err)

	// Generate test chain after a connection to the server.
	// While processing blocks the events will be emitted which is read by subscription
	const chainLength = 30
	for i := 0; i <= chainLength; i++ {
		// Emit event OperatorAdded
		tx, err := boundContract.RegisterOperator(auth, pckd, big.NewInt(100_000_000))
		require.NoError(t, err)
		sim.Commit()
		receipt, err := sim.Client().TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Errorf("get receipt: %v", err)
		}
		require.Equal(t, uint64(0x1), receipt.Status)
	}

	t.Run("client closed", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{
			Ctx: ctx,
		})
		require.NoError(t, err)
		privateKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)
		nodeStorage, operatorData := setupOperatorStorage(logger, db, privateKey)
		require.NoError(t, err)

		eh := setupEventHandler(t, ctx, logger, db, nodeStorage, operatorData, privateKey)

		addr := "ws:" + strings.TrimPrefix(httpSrv.URL, "http:")
		client, err := executionclient.New(ctx, addr, contractAddr, executionclient.WithLogger(logger))
		require.NoError(t, err)

		err = client.Healthy(ctx)
		require.NoError(t, err)

		eventSyncer := New(
			nodeStorage,
			client,
			eh,
			WithLogger(logger),
		)
		eventSyncer.stalenessThreshold = time.Second * 10

		err = nodeStorage.SaveLastProcessedBlock(nil, big.NewInt(1))
		require.NoError(t, err)
		err = eventSyncer.Healthy(ctx)
		require.NoError(t, err)

		lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, 0)
		require.NoError(t, err)
		require.NoError(t, client.Close())
		require.NoError(t, eventSyncer.SyncOngoing(ctx, lastProcessedBlock+1))
	})

	t.Run("context canceled", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{
			Ctx: ctx,
		})
		require.NoError(t, err)
		privateKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)
		nodeStorage, operatorData := setupOperatorStorage(logger, db, privateKey)
		require.NoError(t, err)

		eh := setupEventHandler(t, ctx, logger, db, nodeStorage, operatorData, privateKey)

		addr := "ws:" + strings.TrimPrefix(httpSrv.URL, "http:")
		client, err := executionclient.New(ctx, addr, contractAddr, executionclient.WithLogger(logger))
		require.NoError(t, err)

		err = client.Healthy(ctx)
		require.NoError(t, err)

		eventSyncer := New(
			nodeStorage,
			client,
			eh,
			WithLogger(logger),
		)
		eventSyncer.stalenessThreshold = time.Second * 10

		err = nodeStorage.SaveLastProcessedBlock(nil, big.NewInt(1))
		require.NoError(t, err)

		err = eventSyncer.Healthy(ctx)
		require.NoError(t, err)

		lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, 0)
		require.NoError(t, err)

		cancel()
		require.NoError(t, eventSyncer.SyncOngoing(ctx, lastProcessedBlock+1))
	})
}

func setupEventHandler(
	t *testing.T,
	ctx context.Context,
	logger *zap.Logger,
	db *kv.DB,
	nodeStorage operatorstorage.Storage,
	operatorData *registrystorage.OperatorData,
	privateKey keys.OperatorPrivateKey,
) *eventhandler.EventHandler {
	operatorDataStore := operatordatastore.New(operatorData)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewLocalKeyManager(logger, ekmadapter.NewDatabaseAdapter(db), testNetworkConfig.Beacon, privateKey)
	if err != nil {
		logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
	}

	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{
		Context:           ctx,
		NetworkConfig:     testNetworkConfig,
		DB:                db,
		RegistryStorage:   nodeStorage,
		OperatorDataStore: operatorDataStore,
		ValidatorsMap:     validators.New(ctx),
	})
	// NewController starts ttlcache cleanup goroutines; stop them so they don't leak across tests.
	t.Cleanup(validatorCtrl.Stop)

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	parser, err := eventparser.New(contractFilterer)
	require.NoError(t, err)

	dgHandler := doppelganger.NoOpHandler{}

	eh, err := eventhandler.New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig,
		operatorDataStore,
		keyManager,
		dgHandler,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger))

	if err != nil {
		t.Fatal(err)
	}
	return eh
}

func simTestBackend(testAddr ethcommon.Address) *simulator.Backend {
	return simulator.NewBackend(
		ethtypes.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, simulated.WithBlockGasLimit(10000000),
	)
}

func setupOperatorStorage(logger *zap.Logger, db basedb.Database, privKey keys.OperatorPrivateKey) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	encodedPubKey, err := privKey.Public().Base64()
	if err != nil {
		logger.Fatal("failed to encode operator public key", zap.Error(err))
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, encodedPubKey)
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

func TestBlockBelowThreshold(t *testing.T) {
	ctrl := gomock.NewController(t)
	m := NewMockExecutionClient(ctrl)
	ctx := t.Context()

	s := New(nil, m, nil)

	t.Run("fails on EC error", func(t *testing.T) {
		err1 := errors.New("ec err")
		m.EXPECT().HeaderByNumber(ctx, big.NewInt(1)).Return(nil, err1)
		err := s.ensureBlockAboveThreshold(ctx, big.NewInt(1))
		require.ErrorIs(t, err, err1)
	})

	t.Run("fails if outside threshold", func(t *testing.T) {
		header := &ethtypes.Header{Time: uint64(time.Now().Add(-(defaultStalenessThreshold + time.Second)).Unix())}
		m.EXPECT().HeaderByNumber(ctx, big.NewInt(1)).Return(header, nil)
		err := s.ensureBlockAboveThreshold(ctx, big.NewInt(1))
		require.Error(t, err)
	})

	t.Run("success", func(t *testing.T) {
		header := &ethtypes.Header{Time: uint64(time.Now().Add(-(defaultStalenessThreshold - time.Second)).Unix())}
		m.EXPECT().HeaderByNumber(ctx, big.NewInt(1)).Return(header, nil)
		err := s.ensureBlockAboveThreshold(ctx, big.NewInt(1))
		require.NoError(t, err)
	})
}

func TestSyncOngoingPropagatesErrInferiorBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	executionClient := NewMockExecutionClient(ctrl)
	eventHandlerMock := NewMockEventHandler(ctrl)

	ctx := t.Context()
	syncer := New(nil, executionClient, eventHandlerMock)

	stream := make(chan executionclient.BlockLogs)

	executionClient.EXPECT().
		StreamLogs(ctx, uint64(11)).
		Return(stream)
	eventHandlerMock.EXPECT().
		HandleBlockEventsStream(ctx, stream, true).
		Return(uint64(12), true, fmt.Errorf("process block events: %w", eventhandler.ErrInferiorBlock))

	err := syncer.SyncOngoing(ctx, 11)
	require.Error(t, err)
	require.ErrorIs(t, err, eventhandler.ErrInferiorBlock)
	require.ErrorContains(t, err, "last processed block = 12")
}

// TestSyncHistoryDoesNotRetryHandlerErrors fails the event handler during the history sync. SyncHistory must return
// the error instead of processing the logs again in this process, where memory may still hold changes of the failed
// block.
func TestSyncHistoryDoesNotRetryHandlerErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	executionClient := NewMockExecutionClient(ctrl)
	eventHandlerMock := NewMockEventHandler(ctrl)

	ctx := t.Context()
	syncer := New(nil, executionClient, eventHandlerMock)

	logs := make(chan executionclient.BlockLogs)
	handlerErr := errors.New("signer request failed")

	// Each call is expected once: a retry would fetch the logs again.
	executionClient.EXPECT().
		FetchHistoricalLogs(ctx, uint64(11)).
		Return(logs, make(chan error), nil)
	eventHandlerMock.EXPECT().
		HandleBlockEventsStream(ctx, logs, false).
		Return(uint64(12), true, fmt.Errorf("process block events: %w", handlerErr))

	_, err := syncer.SyncHistory(ctx, 11)
	require.ErrorIs(t, err, handlerErr)
	require.ErrorContains(t, err, "last processed block = 12")
}

// TestSyncHistoryRetriesFetchErrors reports a fetch error after the first attempt has processed its blocks.
// SyncHistory must try again from the block after the last processed one.
func TestSyncHistoryRetriesFetchErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	executionClient := NewMockExecutionClient(ctrl)
	eventHandlerMock := NewMockEventHandler(ctrl)

	ctx := t.Context()
	syncer := New(nil, executionClient, eventHandlerMock)

	firstLogs, firstErrs := make(chan executionclient.BlockLogs), make(chan error, 1)
	firstErrs <- errors.New("fetch failed")
	close(firstErrs)
	secondLogs, secondErrs := make(chan executionclient.BlockLogs), make(chan error)
	close(secondErrs)

	gomock.InOrder(
		executionClient.EXPECT().FetchHistoricalLogs(ctx, uint64(11)).Return(firstLogs, firstErrs, nil),
		eventHandlerMock.EXPECT().HandleBlockEventsStream(ctx, firstLogs, false).Return(uint64(12), true, nil),
		executionClient.EXPECT().FetchHistoricalLogs(ctx, uint64(13)).Return(secondLogs, secondErrs, nil),
		eventHandlerMock.EXPECT().HandleBlockEventsStream(ctx, secondLogs, false).Return(uint64(14), true, nil),
		executionClient.EXPECT().HeaderByNumber(ctx, big.NewInt(14)).Return(&ethtypes.Header{Time: uint64(time.Now().Unix())}, nil),
	)

	lastProcessedBlock, err := syncer.SyncHistory(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, uint64(14), lastProcessedBlock)
}

// TestSyncHistoryRetriesStaleLastBlock finds the first attempt's last processed block older than the staleness
// threshold. SyncHistory must try again from the block after it.
func TestSyncHistoryRetriesStaleLastBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	executionClient := NewMockExecutionClient(ctrl)
	eventHandlerMock := NewMockEventHandler(ctrl)

	ctx := t.Context()
	syncer := New(nil, executionClient, eventHandlerMock)

	firstLogs, firstErrs := make(chan executionclient.BlockLogs), make(chan error)
	close(firstErrs)
	secondLogs, secondErrs := make(chan executionclient.BlockLogs), make(chan error)
	close(secondErrs)
	staleTime := time.Now().Add(-(defaultStalenessThreshold + time.Second))

	gomock.InOrder(
		executionClient.EXPECT().FetchHistoricalLogs(ctx, uint64(11)).Return(firstLogs, firstErrs, nil),
		eventHandlerMock.EXPECT().HandleBlockEventsStream(ctx, firstLogs, false).Return(uint64(12), true, nil),
		executionClient.EXPECT().HeaderByNumber(ctx, big.NewInt(12)).Return(&ethtypes.Header{Time: uint64(staleTime.Unix())}, nil),
		executionClient.EXPECT().FetchHistoricalLogs(ctx, uint64(13)).Return(secondLogs, secondErrs, nil),
		eventHandlerMock.EXPECT().HandleBlockEventsStream(ctx, secondLogs, false).Return(uint64(14), true, nil),
		executionClient.EXPECT().HeaderByNumber(ctx, big.NewInt(14)).Return(&ethtypes.Header{Time: uint64(time.Now().Unix())}, nil),
	)

	lastProcessedBlock, err := syncer.SyncHistory(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, uint64(14), lastProcessedBlock)
}

// TestSyncHistoryHandlerErrorThenRestartStoresBothShares registers two validators in one block, and fails the key manager's first
// request to add the second one's share. SyncHistory must return the error, and after a restart, which reloads memory
// from the database, syncing the block again must store both shares.
func TestSyncHistoryHandlerErrorThenRestartStoresBothShares(t *testing.T) {
	ctx := t.Context()
	logger := zaptest.NewLogger(t)

	db, err := kv.NewInMemory(logger, basedb.Options{Ctx: ctx})
	require.NoError(t, err)

	// loadNodeStorage returns node storage on db, with memory loaded from it as at startup.
	loadNodeStorage := func() operatorstorage.Storage {
		nodeStorage, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
		require.NoError(t, err)
		return nodeStorage
	}

	// Four operators with IDs 1 to 4, of which the node is operator 1.
	operatorKeys := make([]keys.OperatorPrivateKey, 4)
	operators := make([]*registrystorage.OperatorData, len(operatorKeys))
	nodeStorage := loadNodeStorage()
	for i := range operatorKeys {
		operatorKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)
		pubKey, err := operatorKey.Public().Base64()
		require.NoError(t, err)
		operatorKeys[i] = operatorKey
		operators[i] = &registrystorage.OperatorData{ID: uint64(i + 1), PublicKey: pubKey}
		_, err = nodeStorage.SaveOperatorData(nil, operators[i])
		require.NoError(t, err)
	}

	const block = 11
	firstLog, firstValidatorPK, _ := validatorAddedLog(t, operatorKeys, testAddr, 0)
	secondLog, secondValidatorPK, secondShareKey := validatorAddedLog(t, operatorKeys, testAddr, 1)

	ctrl := gomock.NewController(t)
	executionClient := NewMockExecutionClient(ctrl)
	// The block is fetched before the restart, and again after it.
	executionClient.EXPECT().
		FetchHistoricalLogs(ctx, uint64(block)).
		DoAndReturn(func(context.Context, uint64) (<-chan executionclient.BlockLogs, <-chan error, error) {
			logs, errs := make(chan executionclient.BlockLogs, 1), make(chan error)
			logs <- executionclient.BlockLogs{BlockNumber: block, Logs: []ethtypes.Log{firstLog, secondLog}}
			close(logs)
			close(errs)
			return logs, errs, nil
		}).
		Times(2)
	executionClient.EXPECT().
		HeaderByNumber(ctx, big.NewInt(block)).
		Return(&ethtypes.Header{Time: uint64(time.Now().Unix())}, nil)

	localKeyManager, err := ekm.NewLocalKeyManager(logger, ekmadapter.NewDatabaseAdapter(db), networkconfig.TestNetwork.Beacon, operatorKeys[0])
	require.NoError(t, err)
	signerErr := errors.New("signer request failed")
	keyManager := &failOnceKeyManager{KeyManager: localKeyManager, shareKey: secondShareKey, err: signerErr}

	filterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)
	parser, err := eventparser.New(filterer)
	require.NoError(t, err)

	// newSyncer returns the event syncer of a node with the given storage.
	newSyncer := func(storage operatorstorage.Storage) *EventSyncer {
		// The history sync executes no tasks, so the event handler needs no task executor.
		eh, err := eventhandler.New(storage, parser, nil, networkconfig.TestNetwork, operatordatastore.New(operators[0]),
			keyManager, doppelganger.NoOpHandler{}, eventhandler.WithFullNode(), eventhandler.WithLogger(logger))
		require.NoError(t, err)
		return New(storage, executionClient, eh, WithLogger(logger))
	}

	_, err = newSyncer(nodeStorage).SyncHistory(ctx, block)
	require.ErrorIs(t, err, signerErr)

	// The node exits on the error, and syncs the block again after a restart.
	lastProcessedBlock, err := newSyncer(loadNodeStorage()).SyncHistory(ctx, block)
	require.NoError(t, err)
	require.Equal(t, uint64(block), lastProcessedBlock)

	shares := loadNodeStorage().Shares()
	for _, validatorPK := range [][]byte{firstValidatorPK, secondValidatorPK} {
		_, found := shares.Get(nil, validatorPK)
		require.True(t, found)
	}
}

// failOnceKeyManager fails the first request to add the share key shareKey, with err.
type failOnceKeyManager struct {
	ekm.KeyManager
	shareKey phase0.BLSPubKey
	err      error
	failed   bool
}

func (km *failOnceKeyManager) AddShare(ctx context.Context, encryptedPrivKey []byte, pubKey phase0.BLSPubKey) error {
	if pubKey == km.shareKey && !km.failed {
		km.failed = true
		return km.err
	}
	return km.KeyManager.AddShare(ctx, encryptedPrivKey, pubKey)
}

// validatorAddedLog returns the log of owner's registration, at nonce, of a new validator on the operators with IDs 1
// to len(operatorKeys), along with the validator public key and the share public key of operator 1.
func validatorAddedLog(
	t *testing.T,
	operatorKeys []keys.OperatorPrivateKey,
	owner ethcommon.Address,
	nonce int,
) (ethtypes.Log, []byte, phase0.BLSPubKey) {
	t.Helper()
	threshold.Init()

	validatorKey := &bls.SecretKey{}
	validatorKey.SetByCSPRNG()
	quorum, _ := ssvtypes.ComputeQuorumAndPartialQuorum(uint64(len(operatorKeys)))
	shareKeys, err := threshold.Create(validatorKey.Serialize(), quorum, uint64(len(operatorKeys)))
	require.NoError(t, err)

	// The shares hold the signature of owner:nonce, the share public keys, then the encrypted share keys.
	shares := validatorKey.SignByte(crypto.Keccak256([]byte(fmt.Sprintf("%s:%d", owner.String(), nonce)))).Serialize()
	operatorIDs := make([]uint64, len(operatorKeys))
	for i := range operatorKeys {
		operatorIDs[i] = uint64(i + 1)
		shares = append(shares, shareKeys[operatorIDs[i]].GetPublicKey().Serialize()...)
	}
	for i, operatorKey := range operatorKeys {
		encryptedKey, err := operatorKey.Public().Encrypt([]byte(shareKeys[operatorIDs[i]].SerializeToHexStr()))
		require.NoError(t, err)
		shares = append(shares, encryptedKey...)
	}

	contractABI, err := contract.ContractMetaData.GetAbi()
	require.NoError(t, err)
	event := contractABI.Events[eventhandler.ValidatorAdded]
	validatorPK := validatorKey.GetPublicKey().Serialize()
	data, err := event.Inputs.NonIndexed().Pack(operatorIDs, validatorPK, shares, contract.ISSVNetworkCoreCluster{Balance: big.NewInt(0)})
	require.NoError(t, err)

	log := ethtypes.Log{Topics: []ethcommon.Hash{event.ID, ethcommon.BytesToHash(owner.Bytes())}, Data: data}
	return log, validatorPK, phase0.BLSPubKey(shareKeys[1].GetPublicKey().Serialize())
}
