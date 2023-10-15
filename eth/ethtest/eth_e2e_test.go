package ethtest

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
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

	cluster := &simcontract.CallableCluster{
		ValidatorCount:  1,
		NetworkFeeIndex: 1,
		Index:           1,
		Active:          true,
		Balance:         big.NewInt(100_000_000),
	}

	expectedNonce := registrystorage.Nonce(0)

	testEnv := TestEnv{}
	testEnv.SetDefaultFollowDistance()

	defer testEnv.shutdown()
	err := testEnv.setup(t, ctx, testAddresses, 7, 4)
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
		validatorCtrl = testEnv.validatorCtrl
	)

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

			testEnv.CloseFollowDistance(&blockNum)
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
			testEnv.CloseFollowDistance(&blockNum)

			// Run SyncHistory
			lastHandledBlockNum, err = eventSyncer.SyncHistory(ctx, lastHandledBlockNum)
			require.NoError(t, err)

			//check all the events were handled correctly and block number was increased
			require.Equal(t, blockNum-*testEnv.followDistance, lastHandledBlockNum)
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
					return
				case <-stopChan:
					return
				default:
					time.Sleep(100 * time.Millisecond)
				}
			}
		}()

		// Step 1: Add more validators
		{
			validatorCtrl.EXPECT().StartValidator(gomock.Any()).AnyTimes()

			// Check current nonce before start
			nonce, err := nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)

			valAddInput := NewTestValidatorRegisteredInput(common)
			valAddInput.prepare(validators, shares, ops, auth, &expectedNonce, []uint32{2, 3, 4, 5, 6})
			valAddInput.produce()
			testEnv.CloseFollowDistance(&blockNum)

			// Wait until the state is changed
			time.Sleep(time.Millisecond * 5000)

			nonce, err = nodeStorage.GetNextNonce(nil, testAddrAlice)
			require.NoError(t, err)
			require.Equal(t, expectedNonce, nonce)

			// Not sure does this make sense
			require.Equal(t, uint64(testEnv.sim.Blockchain.CurrentBlock().Number.Int64()), *common.blockNum)
		}

		// Step 2: remove validator
		{
			validatorCtrl.EXPECT().StopValidator(gomock.Any()).AnyTimes()

			shares := nodeStorage.Shares().List(nil)
			require.Equal(t, 7, len(shares))

			valRemove := NewTestValidatorRemovedEventsInput(common)
			valRemove.prepare(
				validators,
				[]uint64{0, 1},
				[]uint64{1, 2, 3, 4},
				auth,
				cluster,
			)
			valRemove.produce()
			testEnv.CloseFollowDistance(&blockNum)

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

		// Step 3 Liquidate Cluster
		{
			validatorCtrl.EXPECT().LiquidateCluster(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

			clusterLiquidate := NewTestClusterLiquidatedInput(common)
			clusterLiquidate.prepare([]*ClusterLiquidatedEventInput{
				{
					auth:         auth,
					ownerAddress: &testAddrAlice,
					opsIds:       []uint64{1, 2, 3, 4},
					cluster:      cluster,
				},
			})
			clusterLiquidate.produce()
			testEnv.CloseFollowDistance(&blockNum)

			// Wait until the state is changed
			time.Sleep(time.Millisecond * 300)

			clusterID, err := ssvtypes.ComputeClusterIDHash(testAddrAlice.Bytes(), []uint64{1, 2, 3, 4})
			require.NoError(t, err)

			shares := nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))
			require.NotEmpty(t, shares)
			require.Equal(t, 5, len(shares))

			for _, s := range shares {
				require.True(t, s.Liquidated)
			}
		}

		// Step 4 Reactivate Cluster
		{
			validatorCtrl.EXPECT().ReactivateCluster(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

			clusterID, err := ssvtypes.ComputeClusterIDHash(testAddrAlice.Bytes(), []uint64{1, 2, 3, 4})
			require.NoError(t, err)

			shares := nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))
			require.NotEmpty(t, shares)
			require.Equal(t, 5, len(shares))

			for _, s := range shares {
				require.True(t, s.Liquidated)
			}

			// Trigger the event
			clusterReactivated := NewTestClusterReactivatedInput(common)
			clusterReactivated.prepare([]*ClusterReactivatedEventInput{
				{
					auth:    auth,
					opsIds:  []uint64{1, 2, 3, 4},
					cluster: cluster,
				},
			})
			clusterReactivated.produce()
			testEnv.CloseFollowDistance(&blockNum)

			// Wait until the state is changed
			time.Sleep(time.Millisecond * 300)

			shares = nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))
			require.NotEmpty(t, shares)
			require.Equal(t, 5, len(shares))

			for _, s := range shares {
				require.False(t, s.Liquidated)
			}
		}

		// Step 5 Remove some Operators
		{
			operators, err := nodeStorage.ListOperators(nil, 0, 10)
			require.NoError(t, err)
			require.Equal(t, 4, len(operators))

			opRemoved := NewOperatorRemovedEventInput(common)
			opRemoved.prepare([]uint64{1, 2}, auth)
			opRemoved.produce()
			testEnv.CloseFollowDistance(&blockNum)

			// TODO: this should be adjusted when eth/eventhandler/handlers.go#L109 is resolved
		}

		// Step 6 Update Fee Recipient
		{
			validatorCtrl.EXPECT().UpdateFeeRecipient(gomock.Any(), gomock.Any()).Times(1)

			setFeeRecipient := NewSetFeeRecipientAddressInput(common)
			setFeeRecipient.prepare([]*SetFeeRecipientAddressEventInput{
				{auth, &testAddrBob},
			})
			setFeeRecipient.produce()
			testEnv.CloseFollowDistance(&blockNum)

			// Wait until the state is changed
			time.Sleep(time.Millisecond * 300)

			recipientData, found, err := nodeStorage.GetRecipientData(nil, testAddrAlice)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, testAddrBob.String(), recipientData.FeeRecipient.String())
		}

		stopChan <- struct{}{}
	})
}
