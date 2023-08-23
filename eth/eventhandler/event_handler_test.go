package eventhandler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/operator/validator/mocks"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/utils/blskeygen"
	"github.com/pkg/errors"

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
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/simulator"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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
	eh, err := setupEventHandler(t, ctx, logger)
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
	client, err := executionclient.New(ctx, addr, contractAddr, executionclient.WithLogger(logger), executionclient.WithFollowDistance(0))
	require.NoError(t, err)

	contractFilterer, err := client.Filterer()
	require.NoError(t, err)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)

	logs := client.StreamLogs(ctx, 0)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim)
	require.NoError(t, err)

	ops, err := createOperators(4)
	require.NoError(t, err)
	data, err := generateSharesData(ops)

	blockNum := uint64(0x1)

	t.Run("test OperatorAdded event handle", func(t *testing.T) {

		for _, op := range ops.operators {
			// Call the contract method
			packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op.pub)
			require.NoError(t, err)
			_, err = boundContract.SimcontractTransactor.RegisterOperator(auth, packedOperatorPubKey, big.NewInt(100_000_000))
			require.NoError(t, err)

		}
		sim.Commit()

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		// Check that there is no registered operators
		operators, err := eh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 0, len(operators))

		// Hanlde the event
		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++

		// Check storage for a new operator
		operators, err = eh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, len(ops.operators), len(operators))

		// Check if an operator in the storage has same attributes
		for i, log := range block.Logs {
			operatorAddedEvent, err := contractFilterer.ParseOperatorAdded(log)
			require.NoError(t, err)
			data, _, err := eh.nodeStorage.GetOperatorData(nil, operatorAddedEvent.OperatorId)
			require.NoError(t, err)
			require.Equal(t, operatorAddedEvent.OperatorId, data.ID)
			require.Equal(t, operatorAddedEvent.Owner, data.OwnerAddress)
			require.Equal(t, ops.operators[i].pub, data.PublicKey)
		}
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with error, operator id is correct
	t.Run("test OperatorRemoved event handle", func(t *testing.T) {
		// Call the contract method
		_, err = boundContract.SimcontractTransactor.RemoveOperator(auth, 1)
		require.NoError(t, err)
		sim.Commit()

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		// Check that there is 1 registered operator
		operators, err := eh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, len(ops.operators), len(operators))

		// Hanlde the event
		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++

		// Check if the operator was removed successfuly
		// TODO: this should be adjusted when eth/eventhandler/handlers.go#L109 is resolved
		operators, err = eh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, len(ops.operators), len(operators))
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error,
	// public key is correct, owner is correct, operator ids are correct, shares are correct
	t.Run("test ValidatorAdded event handle", func(t *testing.T) {
		// Call the contract method
		_, err = boundContract.SimcontractTransactor.RegisterValidator(
			auth,
			ops.masterPubKey.Serialize(),
			[]uint64{1, 2, 3, 4},
			data,
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++
		// Check that validator was registered
		shares := eh.nodeStorage.Shares().List(nil)
		require.Equal(t, 1, len(shares))

		malformedShares := data
		malformedShares[len(malformedShares)-1] = 0

		// Call the contract method
		_, err = boundContract.SimcontractTransactor.RegisterValidator(
			auth,
			ops.masterPubKey.Serialize(),
			[]uint64{1, 2, 3, 4},
			malformedShares,
			big.NewInt(100_000_000),
			simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           2,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			})
		require.NoError(t, err)
		sim.Commit()

		block = <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])

		eventsCh = make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err = eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++
		// Check that validator was not registered, but nonce was bumped even event is malformed!
		shares = eh.nodeStorage.Shares().List(nil)
		require.Equal(t, 1, len(shares))
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error,
	// public key is correct, owner is correct, operator ids are correct
	t.Run("test ValidatorRemoved event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.RemoveValidator(
			auth,
			ops.masterPubKey.Serialize(),
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error, owner is correct, operator ids are correct
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error, owner is correct, operator ids are correct
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error, owner is correct, fee recipient is correct
	t.Run("test FeeRecipientAddressUpdated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.SetFeeRecipientAddress(
			auth,
			ethcommon.HexToAddress("0x1"),
		)
		require.NoError(t, err)
		sim.Commit()

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++
		// Check if the fee recepient was updated
		recepientData, _, err := eh.nodeStorage.GetRecipientData(nil, ethcommon.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"))
		require.NoError(t, err)
		require.Equal(t, ethcommon.HexToAddress("0x1").String(), recepientData.FeeRecipient.String())
	})
}

func setupEventHandler(t *testing.T, ctx context.Context, logger *zap.Logger) (*EventHandler, error) {
	db, err := kv.NewInMemory(logger, basedb.Options{
		Ctx: ctx,
	})
	require.NoError(t, err)

	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, true, "")
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
		OperatorData:    operatorData,
	})

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	parser := eventparser.New(contractFilterer)

	eh, err := New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig.Domain,
		validatorCtrl,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		WithFullNode(),
		WithLogger(logger))
	if err != nil {
		return nil, err
	}
	return eh, nil
}

// Copy of setupEventHandler, but with a mocked Validator Controller
func setupEventHandlerWithMockedCtrl(t *testing.T, ctx context.Context, logger *zap.Logger) (*EventHandler, *mocks.MockController, error) {
	db, err := kv.NewInMemory(logger, basedb.Options{
		Ctx: ctx,
	})
	require.NoError(t, err)

	storageMap := ibftstorage.NewStores()
	nodeStorage, _ := setupOperatorStorage(logger, db)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, true, "")
	if err != nil {
		return nil, nil, err
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bc := beacon.NewMockBeaconNode(ctrl)
	validatorCtrl := mocks.NewMockController(ctrl)

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	parser := eventparser.New(contractFilterer)

	eh, err := New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig.Domain,
		validatorCtrl,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		WithFullNode(),
		WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	validatorCtrl.EXPECT().GetOperatorData().Return(&registrystorage.OperatorData{}).AnyTimes()

	return eh, validatorCtrl, nil
}

func setupOperatorStorage(logger *zap.Logger, db basedb.Database) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}
	_, pv, err := rsaencryption.GenerateKeys()
	if err != nil {
		logger.Fatal("failed generating operator key %v", zap.Error(err))
	}
	operatorPubKey, err := nodeStorage.SetupPrivateKey(base64.StdEncoding.EncodeToString(pv))
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

func simTestBackend(testAddr ethcommon.Address) *simulator.SimulatedBackend {
	return simulator.NewSimulatedBackend(
		core.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, 10000000,
	)
}

func TestCreatingSharesData(t *testing.T) {
	ops, err := createOperators(4)
	require.NoError(t, err)
	data, err := generateSharesData(ops)
	require.NoError(t, err)
	operatorCount := 4
	signatureOffset := phase0.SignatureLength
	pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset
	sharesExpectedLength := encryptedKeyLength*operatorCount + pubKeysOffset

	require.Len(t, data, sharesExpectedLength)
}

type testShareData struct {
	masterKey        *bls.SecretKey
	masterPubKey     *bls.PublicKey
	masterPublicKeys bls.PublicKeys
	operators        []*testOperator
}

type testOperator struct {
	id    uint64
	pub   []byte
	priv  []byte
	share testShare
}

type testShare struct {
	sec *bls.SecretKey
	pub *bls.PublicKey
}

//func blsID(opID uint64) bls.ID { //yes, bls.ID is just a struct wrapping a byte array
//	var id bls.ID
//	id.SetDecString(fmt.Sprintf("%d", opID))
//	return id
//}

func createOperators(num uint64) (*testShareData, error) {

	opsAndShares := make(map[uint64]*testShare)

	threshold.Init()

	msk, pubk := blskeygen.GenBLSKeyPair()
	secVec := msk.GetMasterSecretKey(int(num))
	pubks := bls.GetMasterPublicKey(secVec)
	splitKeys, err := threshold.Create(msk.Serialize(), num-1, num)
	if err != nil {
		return nil, err
	}
	// derive a `shareCount` number of shares
	for i := uint64(1); i <= num; i++ {
		var id bls.ID
		err := id.SetDecString(fmt.Sprintf("%d", i))
		if err != nil {
			return nil, err
		}
		var contribPub bls.PublicKey
		err = contribPub.Set(pubks, &id)
		if err != nil {
			return nil, err
		}
		opsAndShares[i] = &testShare{
			sec: splitKeys[i],
			pub: &contribPub,
		}
	}

	testops := make([]*testOperator, num)

	for i := uint64(1); i <= num; i++ {
		pb, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			return nil, err
		}
		testops[i-1] = &testOperator{
			id:   i,
			pub:  pb,
			priv: sk,
			share: testShare{
				opsAndShares[i].sec,
				opsAndShares[i].pub,
			},
		}
	}

	return &testShareData{
		masterKey:        msk,
		masterPubKey:     pubk,
		masterPublicKeys: pubks,
		operators:        testops,
	}, nil
}

func generateSharesData(data *testShareData) ([]byte, error) {
	var pubkeys []byte
	var encryptedShares []byte

	for i, op := range data.operators {
		rsakey, err := rsaencryption.ConvertPemToPublicKey(op.pub)
		if err != nil {
			return nil, fmt.Errorf("cant convert publickey: %w", err)
		}

		rawshare := op.share.sec.SerializeToHexStr()
		ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, rsakey, []byte(rawshare))
		if err != nil {
			return nil, errors.New("cant encrypt share")
		}

		rsapriv, err := rsaencryption.ConvertPemToPrivateKey(string(op.priv))
		if err != nil {
			return nil, err
		}

		// check that we encrypt right
		shareSecret := &bls.SecretKey{}
		decryptedSharePrivateKey, err := rsaencryption.DecodeKey(rsapriv, ciphertext)
		if err != nil {
			return nil, err
		}
		if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
			return nil, err
		}

		pubkeys = append(pubkeys, data.masterPublicKeys[i].Serialize()...)
		encryptedShares = append(encryptedShares, ciphertext...)

	}

	tosign := fmt.Sprintf("%s:%d", testAddr.String(), 0)
	msghash := crypto.Keccak256([]byte(tosign))
	signed := data.masterKey.Sign(string(msghash))
	sig := signed.Serialize()

	if !signed.VerifyByte(data.masterPubKey, msghash) {
		return nil, errors.New("couldn't sign correctly")
	}

	sharesData := append(pubkeys, encryptedShares...)
	sharesDataSigned := append(sig, sharesData...)

	return sharesDataSigned, nil
}
