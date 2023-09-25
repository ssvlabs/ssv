package eth_test

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
	"time"
)

var (
	testKeyAlice, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testKeyBob, _   = crypto.HexToECDSA("42e14d227125f411d6d3285bb4a2e07c2dba2e210bd2f3f4e2a36633bd61bfe6")

	testAddrAlice = crypto.PubkeyToAddress(testKeyAlice.PublicKey)
	testAddrBob   = crypto.PubkeyToAddress(testKeyBob.PublicKey)
)

// E2E tests for ETH package
func TestEthExecLayer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testAddresses := make([]*ethcommon.Address, 2)
	testAddresses[0] = &testAddrAlice
	testAddresses[1] = &testAddrBob
	expectedNonce := registrystorage.Nonce(0)

	testEnv, err := setupEnv(t, ctx, testAddresses, 10, 4)
	require.NoError(t, err)

	var (
		auth          = testEnv.auth
		nodeStorage   = testEnv.nodeStorage
		sim           = testEnv.sim
		boundContract = testEnv.boundContract
		ops           = testEnv.ops
		validators    = testEnv.validators
		eventSyncer   = testEnv.eventSyncer
		shares        = testEnv.shares
		client        = testEnv.execClient
		rpcServer     = testEnv.rpcServer
		httpSrv       = testEnv.httpSrv
	)
	defer rpcServer.Stop()
	defer httpSrv.Close()

	blockNum := uint64(0x1)
	lastHandledBlockNum := uint64(0x1)

	common := NewCommonTestInput(t, sim, boundContract, &blockNum, nodeStorage, true)
	// Prepare blocks with events
	// Check that the state is empty before the test
	// Check SyncHistory doesn't execute any tasks -> doesn't run any of Controller methods
	// Check the node storage for existing of operators and a validator
	t.Run("SyncHistory happy flow", func(t *testing.T) {
		// BLOCK 2. produce OPERATOR ADDED
		// Check that there are no registered operators
		{
			operators, err := nodeStorage.ListOperators(nil, 0, 10)
			require.NoError(t, err)
			require.Equal(t, 0, len(operators))

			opAddedInput := NewOperatorAddedEventInput(common)
			opAddedInput.prepare(ops, auth)
			opAddedInput.produce()
		}

		// BLOCK 3:  VALIDATOR ADDED:
		// Check that there were no operations for Alice Validator
		{
			nonce, err := nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)

			valAddInput := NewTestValidatorRegisteredInput(common)
			valAddInput.prepare(validators, shares, ops, auth, &expectedNonce, []uint32{0, 1})
			valAddInput.produce()

			// Run SyncHistory
			lastHandledBlockNum, err = eventSyncer.SyncHistory(ctx, lastHandledBlockNum)
			require.NoError(t, err)

			//check all the events were handled correctly and block number was increased
			require.Equal(t, blockNum, lastHandledBlockNum)
			fmt.Println("lastHandledBlockNum", lastHandledBlockNum)

			// Check that operators were successfully registered
			operators, err := nodeStorage.ListOperators(nil, 0, 10)
			require.NoError(t, err)
			require.Equal(t, len(ops), len(operators))

			// Check that validator was registered
			shares := nodeStorage.Shares().List(nil)
			require.Equal(t, len(valAddInput.events), len(shares))

			// Check the nonce was bumped
			nonce, err = nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)
		}
	})

	// Main difference between "online" events handling and syncing the historical (old) events
	// is that here we have to check that the controller was triggered
	t.Run("SyncOngoing happy flow", func(t *testing.T) {
		go func() {
			err = eventSyncer.SyncOngoing(ctx, lastHandledBlockNum+1)
			require.NoError(t, err)
		}()

		stopChan := make(chan struct{})
		go func() {
			for {
				select {
				case <-ctx.Done():
					err := client.Close()
					require.NoError(t, err)
					return
				case <-stopChan:
					err := client.Close()
					require.NoError(t, err)
					return
				default:
					time.Sleep(100 * time.Millisecond)
				}
			}
		}()

		// Step 1: Add more validators
		{
			// Check current nonce before start
			nonce, err := nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)

			valAddInput := NewTestValidatorRegisteredInput(common)
			valAddInput.prepare(validators, shares, ops, auth, &expectedNonce, []uint32{2, 3, 4, 5, 6})
			valAddInput.produce()

			// Wait until the state is changed
			time.Sleep(time.Millisecond * 500)

			nonce, err = nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)

			time.Sleep(time.Millisecond * 500)
			nonce, err = nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)

			require.Equal(t, uint64(4), *common.blockNum)
		}

		// Step 2: remove validator
		{
			cluster := &simcontract.CallableCluster{
				ValidatorCount:  1,
				NetworkFeeIndex: 1,
				Index:           2,
				Active:          true,
				Balance:         big.NewInt(100_000_000),
			}

			shares := nodeStorage.Shares().List(nil)
			require.Equal(t, 7, len(shares))

			valRemove := NewTestValidatorRemovedEventsInput(common)
			valRemove.prepare(
				validators,
				[]uint64{0, 1},
				[]uint64{1, 2, 3, 4},
				auth,
				cluster)
			valRemove.produce()

			// Wait until the state is changed
			time.Sleep(time.Millisecond * 500)

			shares = nodeStorage.Shares().List(nil)
			require.Equal(t, 5, len(shares))

			for _, event := range valRemove.events {
				valPubKey := event.validator.masterPubKey.Serialize()
				valShare := nodeStorage.Shares().Get(nil, valPubKey)
				require.Nil(t, valShare)
			}
		}

		stopChan <- struct{}{}
	})
}
