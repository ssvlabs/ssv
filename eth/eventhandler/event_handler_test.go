package eventhandler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	"github.com/bloxapp/ssv/eth/eventparser"
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
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/blskeygen"
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

	// Generate operator key
	operatorPrivKey, operatorPubKey := blskeygen.GenBLSKeyPair()

	// Generate validator shares
	validatorShares, _, err := newValidator()
	require.NoError(t, err)
	// Encode shares
	encodedValidatorShares, err := validatorShares.Encode()
	require.NoError(t, err)
	t.Run("test OperatorAdded event handle", func(t *testing.T) {
		// Call the contract method
		packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(operatorPubKey.Serialize())
		require.NoError(t, err)
		_, err = boundContract.SimcontractTransactor.RegisterOperator(auth, packedOperatorPubKey, big.NewInt(100_000_000))
		require.NoError(t, err)
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
		require.Equal(t, uint64(0x2), lastProcessedBlock)
		require.NoError(t, err)

		// Check storage for a new operator
		operators, err = eh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(operators))

		// Check if an operator in the storage has same attributes
		operatorAddedEvent, err := contractFilterer.ParseOperatorAdded(block.Logs[0])
		require.NoError(t, err)
		data, _, err := eh.nodeStorage.GetOperatorData(nil, operatorAddedEvent.OperatorId)
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
		require.Equal(t, 1, len(operators))

		// Hanlde the event
		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, uint64(0x3), lastProcessedBlock)
		require.NoError(t, err)

		// Check if the operator was removed successfuly
		// TODO: this should be adjusted when eth/eventhandler/handlers.go#L109 is resolved
		operators, err = eh.nodeStorage.ListOperators(nil, 0, 10)
		require.NoError(t, err)
		require.Equal(t, 1, len(operators))
	})

	t.Run("test ValidatorAdded event handle", func(t *testing.T) {
		// pubKey, err := hex.DecodeString(strings.TrimPrefix("0x89913833b5533c1089a957ea185daecc0c5719165f7cbb7ba971c2dcf916be32d6c620dab56888bc278515bf27aebc5f", "0x"))
		// require.NoError(t, err)
		// shares, err := hex.DecodeString(strings.TrimPrefix("0x8d9205986a9f0505daadeb124a4e89814e9174e471da0c8e5a3684c1b2bc7bfb36aca0abe4bf3c619268ce9bdac839d113ae2053f3ce8acbfd5793558efd44709282d423e9b03a1d37a70d27cc4f2a10677448879e6f45a5f8b259e08718b902b52de097cf98c9f6a5355a2060f827e320b1decfbbd974000277de3c65fdeebc4da212ec1def8ef298e7e1459a6ea912b04c8f48f50b65bf2475255cbf4c39b9d1d4dcc34b2959d7b07ed7c493edd22f290483f65f56038c1602d6210a0ad78cb5764bffb0972bc98de742d5f27cd714bbecaae9e398c5322a9b7ca7711c3ce5e952246090e05787f2f91aa2693e118b82bc1b261018b0eb9dcd3c737ccbffb55d726d72d3560d38525b83f882879065badd7cb9504b2e95a205b81c11334e7bab1a5a75539c0cca11e5cf44f51bd989a3ff5c17a68fab85a1f2ea702b667dd3e4526f476c97ad9d2826f291352ebbb8bf1cbd53d47b49ae3f16d75a6eff52184b06caf6662d61c04aca80c9f05e9cdaca8544292df8617a327d519057ace77fe72ba975d6d953d217208d644e5f9a8527f575296b712872c1932492d6bc5519eee9891b22cead112b35c7316070a6ab4c9225559023a3e57d19d7fcd9d9f1641c87d9693389ad50cc335f57f587c3ba4a18760eaea5026d217871192d58a156279b5c764476abe19af43d714474a3bc70af3fc16b0334ec0e411207290b80bd5d61b007e025cd7c640d4637a174e073bf8c357645450f85f614bf42b81dda5f1ebd165717d1c1f4da684f9520dae3702044d0fe84abda960aa1b58127a2aecbe819a9f70d9adbace7c555ec85b4e78b15d50c0b9564d9e0b6abb5e2ed249a8dca3c35fc23c1c71ae045316e95fe19cd24b960f4e1b4f498776dcd244823b5c15ca5000a7990519114dddba550fd457b2438b70ac2d8d62128536a3c5d086a1314a6129157eec322249c82084f2fed4cc6d702e06553c4288dd500463068949401543240bb2d90a57384c0c8a39e004c7ea2ca0f475dc0a0daa330d891198120ff6577c9938e2197c6fecb3974793965fda888bbe94ce6acb1a981e25ef4026d518794cad49e3bd96f7295b526389581b5f5f25de97475eb8c636bdcd4049bbd7bbc4bd8e021d8de33304d586ffb7f5d87f8000372de396d20db43458c91f7ef3e5da35177b1e438426c54235838fa3400fc85f5f5f9e49062ab082db0965e70ed163fa74ce045265c60ec2d86845028dec08e06359b0cd1ccfdf48b0b901cc8d23eecfb5cb6f558277200fffc560282260998a3d510f0e41ced979f5c7e402e41dce60628a23fa7ba68fe2a42921357de525b44d6932e57e65e084e4305bf4524fd06aa825a85ed9ad7db737db963db5deac3a5456a41c75e2848651fbcb04adb0e9c675cf9954f5a56fe3d4cc665b39b63b0011799c7a9a3e01f9a37d4885c05e49f2d5f0fa7559192b0287ad7882c09a08254e56bc4a2f9dc37d6640159596ff468697cd9fe41c1851a4adb92dd4ae8e23e2963adfed4a3a91c916f67e726f1f735a4b5b4309b923817edb668b94bf8462c6c99247e567c4a4ae0475c1c5c116b2622fd961eaf64ea4c9a6aa80f847e7c4a04eb5588d1c969be7bb23ed668e046e7ac14e72427b4cf27f37b44b4037d8cef02321d7b9beb1d7e7c1c08c4083d42c3bf579a97028d85b09aebb3f88aba882fedf8bfdbbb12c1d7221881d8688b95806800bbf1a32b08a81e629ff624fc865558dc6303bae0c74c04aa513b01248d4f22d732f60f7148080a7ccb5388f27fd902ef581b3131932e3997cfa551d61913f3c7a3ce1901ba77a2e61fd5025c4f5762e7d54048d4a9995f0ad6a6e5", "0x"))
		// require.NoError(t, err)
		var tmpShares []byte
		sig := operatorPrivKey.Sign(fmt.Sprintf("%s:%d", ethcommon.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"), 1))
		tmpShares = append(tmpShares, sig.Serialize()...)
		for _, op := range validatorShares.Committee {
			tmpShares = append(tmpShares, op.PubKey...)
		}
		// TODO: do we need the currently unused tmpShares?
		_ = tmpShares

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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, uint64(0x8), lastProcessedBlock)
		require.NoError(t, err)
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
