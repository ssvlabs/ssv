package eventhandler

import (
	"bytes"
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

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ekmcore "github.com/bloxapp/eth2-key-manager/core"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
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
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils"
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

	operatorsCount := uint64(0)
	// Create operators rsa keys
	ops, err := createOperators(4, operatorsCount)
	require.NoError(t, err)
	operatorsCount += uint64(len(ops))

	currentSlot := &utils.SlotValue{}
	mockBeaconNetwork := utils.SetupMockBeaconNetwork(t, currentSlot)
	mockNetworkConfig := &networkconfig.NetworkConfig{
		Beacon: mockBeaconNetwork,
	}

	eh, _, err := setupEventHandler(t, ctx, logger, mockNetworkConfig, ops[0], false)
	if err != nil {
		t.Fatal(err)
	}

	// Just creating one more key -> address for testing
	wrongPk, err := crypto.HexToECDSA("42e14d227125f411d6d3285bb4a2e07c2dba2e210bd2f3f4e2a36633bd61bfe6")
	require.NoError(t, err)
	testAddr2 := crypto.PubkeyToAddress(wrongPk.PublicKey)

	testAddresses := make([]*ethcommon.Address, 2)
	testAddresses[0] = &testAddr
	testAddresses[1] = &testAddr2

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

	err = client.Healthy(ctx)
	require.NoError(t, err)

	logs := client.StreamLogs(ctx, 0)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim)
	require.NoError(t, err)

	// Generate a new validator
	validatorData1, err := createNewValidator(ops)
	require.NoError(t, err)
	sharesData1, err := generateSharesData(validatorData1, ops, testAddr, 0)
	require.NoError(t, err)

	// Create another validator. We'll create the shares later in the tests
	validatorData2, err := createNewValidator(ops)
	require.NoError(t, err)

	validatorData3, err := createNewValidator(ops)
	require.NoError(t, err)
	sharesData3, err := generateSharesData(validatorData3, ops, testAddr, 3)
	require.NoError(t, err)

	blockNum := uint64(0x1)
	currentSlot.SetSlot(100)

	t.Run("test OperatorAdded event handle", func(t *testing.T) {

		for _, op := range ops {
			// Call the contract method
			packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op.rsaPub)
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
		operators, err := eh.nodeStorage.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Equal(t, 0, len(operators))

		// Handle the event
		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++

		// Check storage for the new operators
		operators, err = eh.nodeStorage.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Equal(t, len(ops), len(operators))

		// Check if operators in the storage have same attributes
		for i, log := range block.Logs {
			operatorAddedEvent, err := contractFilterer.ParseOperatorAdded(log)
			require.NoError(t, err)
			data, _, err := eh.nodeStorage.GetOperatorData(nil, operatorAddedEvent.OperatorId)
			require.NoError(t, err)
			require.Equal(t, operatorAddedEvent.OperatorId, data.ID)
			require.Equal(t, operatorAddedEvent.Owner, data.OwnerAddress)
			require.Equal(t, ops[i].rsaPub, data.PublicKey)
		}
	})

	t.Run("test OperatorRemoved event handle", func(t *testing.T) {

		// Should return MalformedEventError and no changes to the state
		t.Run("test OperatorRemoved incorrect operator ID", func(t *testing.T) {
			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RemoveOperator(auth, 100500)
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
			operators, err := eh.nodeStorage.ListOperators(nil, 0, 0)
			require.NoError(t, err)
			require.Equal(t, len(ops), len(operators))

			// Handle the event
			lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			require.NoError(t, err)
			blockNum++

			// Check if the operator wasn't removed successfully
			operators, err = eh.nodeStorage.ListOperators(nil, 0, 0)
			require.NoError(t, err)
			require.Equal(t, len(ops), len(operators))
		})

		// Receive event, unmarshall, parse, check parse event is not nil or with error, operator id is correct
		// TODO: fix this test. It checks nothing, due the handleOperatorRemoved method is no-op currently
		t.Run("test OperatorRemoved happy flow", func(t *testing.T) {
			// Prepare a new operator to remove it later in this test
			op, err := createOperators(1, operatorsCount)
			require.NoError(t, err)
			operatorsCount++

			// Call the contract method
			packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op[0].rsaPub)
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
			operators, err := eh.nodeStorage.ListOperators(nil, 0, 0)
			require.NoError(t, err)
			require.Equal(t, len(ops), len(operators))

			// Handle OperatorAdded event
			lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			require.NoError(t, err)
			blockNum++
			// Check storage for the new operator
			operators, err = eh.nodeStorage.ListOperators(nil, 0, 0)
			require.NoError(t, err)
			require.Equal(t, len(ops)+1, len(operators))

			// Now start the OperatorRemoved event handling
			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RemoveOperator(auth, 4)
			require.NoError(t, err)
			sim.Commit()

			block = <-logs
			require.NotEmpty(t, block.Logs)
			require.Equal(t, ethcommon.HexToHash("0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e"), block.Logs[0].Topics[0])

			eventsCh = make(chan executionclient.BlockLogs)
			go func() {
				defer close(eventsCh)
				eventsCh <- block
			}()

			operators, err = eh.nodeStorage.ListOperators(nil, 0, 0)
			require.NoError(t, err)
			require.Equal(t, len(ops)+1, len(operators))

			// Handle OperatorRemoved event
			lastProcessedBlock, err = eh.HandleBlockEventsStream(eventsCh, false)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			require.NoError(t, err)
			blockNum++

			// TODO: this should be adjusted when eth/eventhandler/handlers.go#L109 is resolved
			// Check if the operator was removed successfully
			//operators, err = eh.nodeStorage.ListOperators(nil, 0, 0)
			//require.NoError(t, err)
			//require.Equal(t, len(ops), len(operators))
		})
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error,
	// public key is correct, owner is correct, operator ids are correct, shares are correct
	// slashing protection data is correct
	t.Run("test ValidatorAdded event handle", func(t *testing.T) {
		nonce, err := eh.nodeStorage.GetNextNonce(nil, testAddr)
		require.NoError(t, err)
		require.Equal(t, registrystorage.Nonce(0), nonce)

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

		block := <-logs
		require.NotEmpty(t, block.Logs)
		require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])

		eventsCh := make(chan executionclient.BlockLogs)
		go func() {
			defer close(eventsCh)
			eventsCh <- block
		}()

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.NoError(t, err)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		blockNum++

		requireKeyManagerDataToExist(t, eh, 1, validatorData1)

		// Check that validator was registered
		shares := eh.nodeStorage.Shares().List(nil)
		require.Equal(t, 1, len(shares))
		// Check the nonce was bumped
		nonce, err = eh.nodeStorage.GetNextNonce(nil, testAddr)
		require.NoError(t, err)
		require.Equal(t, registrystorage.Nonce(1), nonce)

		sharesData2, err := generateSharesData(validatorData2, ops, testAddr, 2)
		require.NoError(t, err)

		// SharesData length is incorrect. Nonce is bumped; Validator wasn't added
		// slashing protection data is not added
		t.Run("test nonce bumping even for incorrect sharesData length", func(t *testing.T) {
			// changing the length
			malformedSharesData := sharesData2[:len(sharesData2)-1]

			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RegisterValidator(
				auth,
				validatorData2.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				malformedSharesData,
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
			require.NoError(t, err)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			blockNum++

			requireKeyManagerDataToNotExist(t, eh, 1, validatorData2)

			// Check that validator was not registered,
			shares = eh.nodeStorage.Shares().List(nil)
			require.Equal(t, 1, len(shares))
			// but nonce was bumped even the event is malformed!
			nonce, err = eh.nodeStorage.GetNextNonce(nil, testAddr)
			require.NoError(t, err)
			require.Equal(t, registrystorage.Nonce(2), nonce)
		})

		// Length of the shares []byte is correct; nonce is bumped; validator is added
		// slashing protection data is correct
		t.Run("test validator 1 doesnt check validator's 4 share", func(t *testing.T) {
			malformedSharesData := sharesData2[:]
			// Corrupt the encrypted last share key of the 4th operator
			malformedSharesData[len(malformedSharesData)-1] ^= 1

			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RegisterValidator(
				auth,
				validatorData2.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				malformedSharesData,
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
			require.NoError(t, err)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			blockNum++

			requireKeyManagerDataToExist(t, eh, 2, validatorData2)

			// Check that validator was registered for op1,
			shares = eh.nodeStorage.Shares().List(nil)
			require.Equal(t, 2, len(shares))
			// and nonce was bumped
			nonce, err = eh.nodeStorage.GetNextNonce(nil, testAddr)
			require.NoError(t, err)
			require.Equal(t, registrystorage.Nonce(3), nonce)
		})

		// Share for 1st operator is malformed; check nonce is bumped correctly; validator wasn't added
		// slashing protection data is not added
		t.Run("test malformed ValidatorAdded and nonce is bumped", func(t *testing.T) {
			malformedSharesData := sharesData3[:]

			operatorCount := len(ops)
			signatureOffset := phase0.SignatureLength
			pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset

			// Corrupt the encrypted share key of the operator 1
			malformedSharesData[pubKeysOffset+encryptedKeyLength-1] ^= 1

			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RegisterValidator(
				auth,
				validatorData3.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				malformedSharesData,
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
			require.NoError(t, err)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			blockNum++

			requireKeyManagerDataToNotExist(t, eh, 2, validatorData3)

			// Check that validator was not registered
			shares = eh.nodeStorage.Shares().List(nil)
			require.Equal(t, 2, len(shares))
			// and nonce was bumped
			nonce, err = eh.nodeStorage.GetNextNonce(nil, testAddr)
			require.NoError(t, err)
			require.Equal(t, registrystorage.Nonce(4), nonce)
		})

		// Correct event; check nonce is bumped correctly; validator is added
		// slashing protection data is correct
		t.Run("test correct ValidatorAdded again and nonce is bumped", func(t *testing.T) {
			// regenerate with updated nonce
			sharesData3, err = generateSharesData(validatorData3, ops, testAddr, 4)
			require.NoError(t, err)
			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RegisterValidator(
				auth,
				validatorData3.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				sharesData3,
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
			require.NoError(t, err)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			blockNum++

			requireKeyManagerDataToExist(t, eh, 3, validatorData3)

			// Check that validator was registered
			shares = eh.nodeStorage.Shares().List(nil)
			require.Equal(t, 3, len(shares))
			// and nonce was bumped
			nonce, err = eh.nodeStorage.GetNextNonce(nil, testAddr)
			require.NoError(t, err)
			require.Equal(t, registrystorage.Nonce(5), nonce)
		})

		t.Run("test correct ValidatorAdded again and nonce is bumped with another owner", func(t *testing.T) {
			validatorData4, err := createNewValidator(ops)
			require.NoError(t, err)
			authTestAddr2, _ := bind.NewKeyedTransactorWithChainID(wrongPk, big.NewInt(1337))

			sharesData4, err := generateSharesData(validatorData4, ops, testAddr2, 0)
			require.NoError(t, err)
			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RegisterValidator(
				authTestAddr2,
				validatorData4.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				sharesData4,
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
			require.NoError(t, err)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			blockNum++

			requireKeyManagerDataToExist(t, eh, 4, validatorData4)

			// Check that validator was registered
			shares = eh.nodeStorage.Shares().List(nil)
			require.Equal(t, 4, len(shares))
			// and nonce was bumped
			nonce, err = eh.nodeStorage.GetNextNonce(nil, testAddr2)
			require.NoError(t, err)
			// Check that nonces are not intertwined between different owner accounts!
			require.Equal(t, registrystorage.Nonce(1), nonce)
		})

	})

	t.Run("test ValidatorRemoved event handling", func(t *testing.T) {
		// Must throw error "malformed event: could not find validator share"
		t.Run("ValidatorRemoved incorrect event public key", func(t *testing.T) {
			pk := validatorData1.masterPubKey.Serialize()
			// Corrupt the public key
			pk[len(pk)-1] ^= 1

			_, err = boundContract.SimcontractTransactor.RemoveValidator(
				auth,
				pk,
				[]uint64{1, 2, 3, 4},
				simcontract.CallableCluster{
					ValidatorCount:  1,
					NetworkFeeIndex: 1,
					Index:           2,
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

			// Check the validator's shares are still present in the state after incorrect ValidatorRemoved event
			valShare := eh.nodeStorage.Shares().Get(nil, validatorData1.masterPubKey.Serialize())
			require.NotNil(t, valShare)
		})

		t.Run("ValidatorRemoved incorrect owner address", func(t *testing.T) {
			wrongAuth, _ := bind.NewKeyedTransactorWithChainID(wrongPk, big.NewInt(1337))

			_, err = boundContract.SimcontractTransactor.RemoveValidator(
				wrongAuth,
				validatorData1.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				simcontract.CallableCluster{
					ValidatorCount:  1,
					NetworkFeeIndex: 1,
					Index:           2,
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

			// Check the validator's shares are still present in the state after incorrect ValidatorRemoved event
			valShare := eh.nodeStorage.Shares().Get(nil, validatorData1.masterPubKey.Serialize())
			require.NotNil(t, valShare)
		})

		// Receive event, unmarshall, parse, check parse event is not nil or with an error,
		// public key is correct, owner is correct, operator ids are correct
		// event handler's own operator is responsible for removed validator
		t.Run("ValidatorRemoved happy flow", func(t *testing.T) {
			valPubKey := validatorData1.masterPubKey.Serialize()
			// Check the validator's shares are present in the state before removing
			valShare := eh.nodeStorage.Shares().Get(nil, valPubKey)
			require.NotNil(t, valShare)
			requireKeyManagerDataToExist(t, eh, 4, validatorData1)

			_, err = boundContract.SimcontractTransactor.RemoveValidator(
				auth,
				validatorData1.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				simcontract.CallableCluster{
					ValidatorCount:  1,
					NetworkFeeIndex: 1,
					Index:           2,
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

			// Check the validator was removed from the validator shares storage.
			shares := eh.nodeStorage.Shares().List(nil)
			require.Equal(t, 3, len(shares))
			valShare = eh.nodeStorage.Shares().Get(nil, valPubKey)
			require.Nil(t, valShare)
			requireKeyManagerDataToNotExist(t, eh, 3, validatorData1)
		})
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error, owner is correct, operator ids are correct
	// slashing protection data is not deleted
	t.Run("test ClusterLiquidated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.Liquidate(
			auth,
			testAddr,
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

		// Using validator 2 because we've removed validator 1 in ValidatorRemoved tests. This one has to be in the state
		valPubKey := validatorData2.masterPubKey.Serialize()

		share := eh.nodeStorage.Shares().Get(nil, valPubKey)
		require.NotNil(t, share)
		require.False(t, share.Liquidated)

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)
		blockNum++

		share = eh.nodeStorage.Shares().Get(nil, valPubKey)
		require.NotNil(t, share)
		require.True(t, share.Liquidated)
		// check that slashing data was not deleted
		sharePubKey := validatorData3.operatorsShares[0].sec.GetPublicKey().Serialize()
		highestAttestation, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestAttestation(sharePubKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, highestAttestation)

		require.Equal(t, highestAttestation.Source.Epoch, mockBeaconNetwork.EstimatedEpochAtSlot(currentSlot.GetSlot())-1)
		require.Equal(t, highestAttestation.Target.Epoch, mockBeaconNetwork.EstimatedEpochAtSlot(currentSlot.GetSlot()))

		highestProposal, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestProposal(sharePubKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, highestProposal, currentSlot.GetSlot())
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error, owner is correct, operator ids are correct
	// **  storedEpoch = max(nextEpoch, storedEpoch)  **
	// Validate that slashing protection data stored epoch is nextEpoch and NOT storedEpoch
	t.Run("test ClusterReactivated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.Reactivate(
			auth,
			[]uint64{1, 2, 3, 4},
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

		currentSlot.SetSlot(1000)

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)

		// check that slashing data was bumped
		sharePubKey := validatorData3.operatorsShares[0].sec.GetPublicKey().Serialize()
		highestAttestation, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestAttestation(sharePubKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, highestAttestation)
		require.Equal(t, highestAttestation.Source.Epoch, mockBeaconNetwork.EstimatedEpochAtSlot(currentSlot.GetSlot())-1)
		require.Equal(t, highestAttestation.Target.Epoch, mockBeaconNetwork.EstimatedEpochAtSlot(currentSlot.GetSlot()))

		highestProposal, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestProposal(sharePubKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, highestProposal, currentSlot.GetSlot())

		blockNum++
	})

	// Liquidated event is far in the future
	// in order to simulate stored far in the future slashing protection data
	t.Run("test ClusterLiquidated event handle - far in the future", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.Liquidate(
			auth,
			testAddr,
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

	// Reactivate event
	// **  storedEpoch = max(nextEpoch, storedEpoch)  **
	// Validate that slashing protection data stored epoch is storedEpoch and NOT nextEpoch
	t.Run("test ClusterReactivated event handle - far in the future", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.Reactivate(
			auth,
			[]uint64{1, 2, 3, 4},
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

		// Using validator 2 because we've removed validator 1 in ValidatorRemoved tests
		valPubKey := validatorData2.masterPubKey.Serialize()

		share := eh.nodeStorage.Shares().Get(nil, valPubKey)
		require.NotNil(t, share)
		require.True(t, share.Liquidated)
		currentSlot.SetSlot(100)

		lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
		require.Equal(t, blockNum+1, lastProcessedBlock)
		require.NoError(t, err)

		// check that slashing data is greater than current epoch
		sharePubKey := validatorData3.operatorsShares[0].sec.GetPublicKey().Serialize()
		highestAttestation, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestAttestation(sharePubKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, highestAttestation)
		require.Greater(t, highestAttestation.Source.Epoch, mockBeaconNetwork.EstimatedEpochAtSlot(currentSlot.GetSlot())-1)
		require.Greater(t, highestAttestation.Target.Epoch, mockBeaconNetwork.EstimatedEpochAtSlot(currentSlot.GetSlot()))

		highestProposal, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestProposal(sharePubKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Greater(t, highestProposal, currentSlot.GetSlot())

		blockNum++

		share = eh.nodeStorage.Shares().Get(nil, valPubKey)
		require.NotNil(t, share)
		require.False(t, share.Liquidated)
	})

	// Receive event, unmarshall, parse, check parse event is not nil or with an error, owner is correct, fee recipient is correct
	t.Run("test FeeRecipientAddressUpdated event handle", func(t *testing.T) {
		_, err = boundContract.SimcontractTransactor.SetFeeRecipientAddress(
			auth,
			testAddr2,
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
		// Check if the fee recipient was updated
		recipientData, _, err := eh.nodeStorage.GetRecipientData(nil, testAddr)
		require.NoError(t, err)
		require.Equal(t, testAddr2.String(), recipientData.FeeRecipient.String())
	})

	// DO / UNDO in one block tests
	t.Run("test DO / UNDO in one block", func(t *testing.T) {
		t.Run("test OperatorAdded + OperatorRemoved events handling", func(t *testing.T) {
			// There are 5 ops before the test running
			// Check that there is no registered operators
			operators, err := eh.nodeStorage.ListOperators(nil, 0, 0)
			require.NoError(t, err)
			require.Equal(t, operatorsCount, uint64(len(operators)))

			tmpOps, err := createOperators(1, operatorsCount)
			require.NoError(t, err)
			operatorsCount++
			op := tmpOps[0]

			// Call the RegisterOperator contract method
			packedOperatorPubKey, err := eventparser.PackOperatorPublicKey(op.rsaPub)
			require.NoError(t, err)
			_, err = boundContract.SimcontractTransactor.RegisterOperator(auth, packedOperatorPubKey, big.NewInt(100_000_000))
			require.NoError(t, err)

			// Call the OperatorRemoved contract method
			_, err = boundContract.SimcontractTransactor.RemoveOperator(auth, op.id)
			require.NoError(t, err)

			sim.Commit()

			block := <-logs
			require.NotEmpty(t, block.Logs)
			require.Equal(t, ethcommon.HexToHash("0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4"), block.Logs[0].Topics[0])
			require.Equal(t, ethcommon.HexToHash("0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e"), block.Logs[1].Topics[0])

			eventsCh := make(chan executionclient.BlockLogs)
			go func() {
				defer close(eventsCh)
				eventsCh <- block
			}()

			// Handle the event
			lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			require.NoError(t, err)
			blockNum++

			// #TODO: Fails until we fix the OperatorAdded: handlers.go #108
			// Check storage for the new operators
			//operators, err = eh.nodeStorage.ListOperators(nil, 0, 0)
			//require.NoError(t, err)
			//require.Equal(t, operatorsCount-1, uint64(len(operators)))
			//
			//_, found, err := eh.nodeStorage.GetOperatorData(nil, op.id)
			//require.NoError(t, err)
			//require.False(t, found)
		})

		t.Run("test ValidatorAdded + ValidatorRemoved events handling", func(t *testing.T) {
			shares := eh.nodeStorage.Shares().List(nil)
			sharesCountBeforeTest := len(shares)

			validatorData4, err := createNewValidator(ops)
			require.NoError(t, err)

			currentNonce, err := eh.nodeStorage.GetNextNonce(nil, testAddr)
			require.NoError(t, err)

			sharesData4, err := generateSharesData(validatorData4, ops, testAddr, int(currentNonce))
			require.NoError(t, err)

			valPubKey := validatorData4.masterPubKey.Serialize()
			valShare := eh.nodeStorage.Shares().Get(nil, valPubKey)
			require.Nil(t, valShare)

			// Call the contract method
			_, err = boundContract.SimcontractTransactor.RegisterValidator(
				auth,
				validatorData4.masterPubKey.Serialize(),
				[]uint64{1, 2, 3, 4},
				sharesData4,
				big.NewInt(100_000_000),
				simcontract.CallableCluster{
					ValidatorCount:  1,
					NetworkFeeIndex: 1,
					Index:           2,
					Active:          true,
					Balance:         big.NewInt(100_000_000),
				})
			require.NoError(t, err)

			_, err = boundContract.SimcontractTransactor.RemoveValidator(
				auth,
				valPubKey,
				[]uint64{1, 2, 3, 4},
				simcontract.CallableCluster{
					ValidatorCount:  1,
					NetworkFeeIndex: 1,
					Index:           2,
					Active:          true,
					Balance:         big.NewInt(100_000_000),
				})

			require.NoError(t, err)

			sim.Commit()

			block := <-logs
			require.NotEmpty(t, block.Logs)
			require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])
			require.Equal(t, ethcommon.HexToHash("0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e"), block.Logs[1].Topics[0])

			eventsCh := make(chan executionclient.BlockLogs)
			go func() {
				defer close(eventsCh)
				eventsCh <- block
			}()

			lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			require.NoError(t, err)
			blockNum++

			valShare = eh.nodeStorage.Shares().Get(nil, valPubKey)
			require.Nil(t, valShare)

			// Check that validator was registered
			shares = eh.nodeStorage.Shares().List(nil)
			require.Equal(t, sharesCountBeforeTest, len(shares))
			// and nonce was bumped
			nonce, err := eh.nodeStorage.GetNextNonce(nil, testAddr)
			require.NoError(t, err)
			require.Equal(t, currentNonce+1, nonce)
		})

		t.Run("test ClusterLiquidated + ClusterReactivated events handling", func(t *testing.T) {
			// Using validator 2 because we've removed validator 1 in ValidatorRemoved tests
			valPubKey := validatorData2.masterPubKey.Serialize()
			share := eh.nodeStorage.Shares().Get(nil, valPubKey)

			require.NotNil(t, share)
			require.False(t, share.Liquidated)
			_, err = boundContract.SimcontractTransactor.Liquidate(
				auth,
				testAddr,
				[]uint64{1, 2, 3, 4},
				simcontract.CallableCluster{
					ValidatorCount:  1,
					NetworkFeeIndex: 1,
					Index:           1,
					Active:          true,
					Balance:         big.NewInt(100_000_000),
				})
			require.NoError(t, err)

			_, err = boundContract.SimcontractTransactor.Reactivate(
				auth,
				[]uint64{1, 2, 3, 4},
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
			require.Equal(t, ethcommon.HexToHash("0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688"), block.Logs[0].Topics[0])
			require.Equal(t, ethcommon.HexToHash("0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859"), block.Logs[1].Topics[0])

			eventsCh := make(chan executionclient.BlockLogs)
			go func() {
				defer close(eventsCh)
				eventsCh <- block
			}()

			lastProcessedBlock, err := eh.HandleBlockEventsStream(eventsCh, false)
			require.Equal(t, blockNum+1, lastProcessedBlock)
			require.NoError(t, err)
			blockNum++

			share = eh.nodeStorage.Shares().Get(nil, valPubKey)
			require.NotNil(t, share)
			require.False(t, share.Liquidated)
		})
	})
}

func setupEventHandler(t *testing.T, ctx context.Context, logger *zap.Logger, network *networkconfig.NetworkConfig, operator *testOperator, useMockCtrl bool) (*EventHandler, *mocks.MockController, error) {
	db, err := kv.NewInMemory(logger, basedb.Options{
		Ctx: ctx,
	})
	require.NoError(t, err)

	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db, operator)

	if network == nil {
		network = &networkconfig.NetworkConfig{
			Beacon: utils.SetupMockBeaconNetwork(t, &utils.SlotValue{}),
		}
	}

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, *network, true, "")
	if err != nil {
		return nil, nil, err
	}

	if useMockCtrl {
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
			network.Domain,
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
		ValidatorsMap:   validatorsmap.New(ctx),
	})

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	parser := eventparser.New(contractFilterer)

	eh, err := New(
		nodeStorage,
		parser,
		validatorCtrl,
		network.Domain,
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
	return eh, nil, nil
}

func setupOperatorStorage(logger *zap.Logger, db basedb.Database, operator *testOperator) (operatorstorage.Storage, *registrystorage.OperatorData) {
	if operator == nil {
		logger.Fatal("empty test operator was passed")
	}

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	operatorPubKey, err := nodeStorage.SetupPrivateKey(base64.StdEncoding.EncodeToString(operator.rsaPriv))
	if err != nil {
		logger.Fatal("couldn't setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(nil, operatorPubKey)

	if err != nil {
		logger.Fatal("couldn't get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey:    operatorPubKey,
			ID:           operator.id,
			OwnerAddress: testAddr,
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

func simTestBackend(testAddresses []*ethcommon.Address) *simulator.SimulatedBackend {
	genesis := core.GenesisAlloc{}

	for _, testAddr := range testAddresses {
		genesis[*testAddr] = core.GenesisAccount{Balance: big.NewInt(10000000000000000)}
	}

	return simulator.NewSimulatedBackend(
		genesis, 50_000_000,
	)
}

func TestCreatingSharesData(t *testing.T) {
	owner := testAddr
	nonce := 0
	ops, err := createOperators(4, 1)
	require.NoError(t, err)

	validatorData, err := createNewValidator(ops)
	require.NoError(t, err)
	// TODO: maybe we can merge createNewValidator and generateSharesData
	sharesData, err := generateSharesData(validatorData, ops, owner, nonce)

	require.NoError(t, err)
	operatorCount := len(ops)
	signatureOffset := phase0.SignatureLength
	pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset
	sharesExpectedLength := encryptedKeyLength*operatorCount + pubKeysOffset

	require.Len(t, sharesData, sharesExpectedLength)

	signature := sharesData[:signatureOffset]

	err = verifySignature(signature, owner, validatorData.masterPubKey.Serialize(), registrystorage.Nonce(nonce))
	require.NoError(t, err)

	sharePublicKeys := splitBytes(sharesData[signatureOffset:pubKeysOffset], phase0.PublicKeyLength)
	encryptedKeys := splitBytes(sharesData[pubKeysOffset:], len(sharesData[pubKeysOffset:])/operatorCount)

	for i, enck := range encryptedKeys {
		priv, err := rsaencryption.ConvertPemToPrivateKey(string(ops[i].rsaPriv))
		require.NoError(t, err)
		decryptedSharePrivateKey, err := rsaencryption.DecodeKey(priv, enck)
		require.NoError(t, err)
		share := &bls.SecretKey{}
		require.NoError(t, share.SetHexString(string(decryptedSharePrivateKey)))

		require.Equal(t, validatorData.operatorsShares[i].sec.SerializeToHexStr(), string(decryptedSharePrivateKey))
		require.Equal(t, validatorData.operatorsShares[i].pub.Serialize(), sharePublicKeys[i])
		require.Equal(t, share.GetPublicKey().Serialize(), sharePublicKeys[i])
	}
}

type testValidatorData struct {
	masterKey        *bls.SecretKey
	masterPubKey     *bls.PublicKey
	masterPublicKeys bls.PublicKeys
	operatorsShares  []*testShare
}

type testOperator struct {
	id      uint64
	rsaPub  []byte
	rsaPriv []byte
}

type testShare struct {
	opId uint64
	sec  *bls.SecretKey
	pub  *bls.PublicKey
}

func shareExist(accounts []ekmcore.ValidatorAccount, sharePubKey []byte) bool {
	for _, acc := range accounts {
		if bytes.Equal(acc.ValidatorPublicKey(), sharePubKey) {
			return true
		}
	}
	return false
}

func createNewValidator(ops []*testOperator) (*testValidatorData, error) {
	validatorData := &testValidatorData{}
	sharesCount := uint64(len(ops))
	threshold.Init()

	msk, mpk := blskeygen.GenBLSKeyPair()
	secVec := msk.GetMasterSecretKey(int(sharesCount))
	pubKeys := bls.GetMasterPublicKey(secVec)
	splitKeys, err := threshold.Create(msk.Serialize(), sharesCount-1, sharesCount)
	if err != nil {
		return nil, err
	}

	validatorData.operatorsShares = make([]*testShare, sharesCount)

	// derive a `sharesCount` number of shares
	for i := uint64(1); i <= sharesCount; i++ {
		validatorData.operatorsShares[i-1] = &testShare{
			opId: i,
			sec:  splitKeys[i],
			pub:  splitKeys[i].GetPublicKey(),
		}
	}

	validatorData.masterKey = msk
	validatorData.masterPubKey = mpk
	validatorData.masterPublicKeys = pubKeys

	return validatorData, nil
}

func createOperators(num uint64, idOffset uint64) ([]*testOperator, error) {
	testOps := make([]*testOperator, num)

	for i := uint64(1); i <= num; i++ {
		pb, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			return nil, err
		}
		testOps[i-1] = &testOperator{
			id:      idOffset + i,
			rsaPub:  pb,
			rsaPriv: sk,
		}
	}

	return testOps, nil
}

func generateSharesData(validatorData *testValidatorData, operators []*testOperator, owner ethcommon.Address, nonce int) ([]byte, error) {
	var pubKeys []byte
	var encryptedShares []byte

	for i, op := range operators {
		rsaKey, err := rsaencryption.ConvertPemToPublicKey(op.rsaPub)
		if err != nil {
			return nil, fmt.Errorf("can't convert public key: %w", err)
		}

		rawShare := validatorData.operatorsShares[i].sec.SerializeToHexStr()
		cipherText, err := rsa.EncryptPKCS1v15(rand.Reader, rsaKey, []byte(rawShare))
		if err != nil {
			return nil, fmt.Errorf("can't encrypt share: %w", err)
		}

		rsaPriv, err := rsaencryption.ConvertPemToPrivateKey(string(op.rsaPriv))
		if err != nil {
			return nil, fmt.Errorf("can't convert secret key to a private key share: %w", err)
		}

		// check that we encrypt right
		shareSecret := &bls.SecretKey{}
		decryptedSharePrivateKey, err := rsaencryption.DecodeKey(rsaPriv, cipherText)
		if err != nil {
			return nil, err
		}
		if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
			return nil, err
		}

		pubKeys = append(pubKeys, validatorData.operatorsShares[i].pub.Serialize()...)
		encryptedShares = append(encryptedShares, cipherText...)

	}

	toSign := fmt.Sprintf("%s:%d", owner.String(), nonce)
	msgHash := crypto.Keccak256([]byte(toSign))
	signed := validatorData.masterKey.Sign(string(msgHash))
	sig := signed.Serialize()

	if !signed.VerifyByte(validatorData.masterPubKey, msgHash) {
		return nil, errors.New("can't sign correctly")
	}

	sharesData := append(pubKeys, encryptedShares...)
	sharesDataSigned := append(sig, sharesData...)

	return sharesDataSigned, nil
}

func requireKeyManagerDataToExist(t *testing.T, eh *EventHandler, expectedAccounts int, validatorData *testValidatorData) {
	sharePubKey := validatorData.operatorsShares[0].sec.GetPublicKey().Serialize()
	accounts, err := eh.keyManager.(ekm.StorageProvider).ListAccounts()
	require.NoError(t, err)
	require.Equal(t, expectedAccounts, len(accounts))
	require.True(t, shareExist(accounts, sharePubKey))

	highestAttestation, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestAttestation(sharePubKey)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, highestAttestation)

	_, found, err = eh.keyManager.(ekm.StorageProvider).RetrieveHighestProposal(sharePubKey)
	require.NoError(t, err)
	require.True(t, found)
}

func requireKeyManagerDataToNotExist(t *testing.T, eh *EventHandler, expectedAccounts int, validatorData *testValidatorData) {
	sharePubKey := validatorData.operatorsShares[0].sec.GetPublicKey().Serialize()
	accounts, err := eh.keyManager.(ekm.StorageProvider).ListAccounts()
	require.NoError(t, err)
	require.Equal(t, expectedAccounts, len(accounts))
	require.False(t, shareExist(accounts, sharePubKey))

	highestAttestation, found, err := eh.keyManager.(ekm.StorageProvider).RetrieveHighestAttestation(sharePubKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, highestAttestation)

	_, found, err = eh.keyManager.(ekm.StorageProvider).RetrieveHighestProposal(sharePubKey)
	require.NoError(t, err)
	require.False(t, found)
}
