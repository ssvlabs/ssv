package fee_recipient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/operator/slotticker/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// mockValidatorProvider is a test implementation of ValidatorProvider
type mockValidatorProvider struct {
	shareStorage registrystorage.Shares
	operatorID   spectypes.OperatorID
}

func (m *mockValidatorProvider) SelfValidators() []*types.SSVShare {
	// Return all shares for the operator
	var shares []*types.SSVShare
	m.shareStorage.Range(nil, func(share *types.SSVShare) bool {
		for _, op := range share.Committee {
			if op.Signer == m.operatorID {
				shares = append(shares, share)
				break
			}
		}
		return true
	})
	return shares
}

func TestSubmitProposal(t *testing.T) {
	logger := log.TestLogger(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	operatorData := &registrystorage.OperatorData{
		ID: 123456789,
	}

	operatorDataStore := operatordatastore.New(operatorData)

	db, shareStorage, recipientStorage := createStorage(t)
	defer db.Close()

	beaconConfig := networkconfig.TestNetwork.Beacon
	populateStorage(t, shareStorage, operatorData)

	// Create a mock validator provider
	mockValidatorProvider := &mockValidatorProvider{shareStorage: shareStorage, operatorID: operatorData.ID}

	frCtrl := NewController(logger, &ControllerOptions{
		Ctx:               context.TODO(),
		BeaconConfig:      beaconConfig,
		ValidatorProvider: mockValidatorProvider,
		RecipientStorage:  recipientStorage,
		OperatorDataStore: operatorDataStore,
	})

	t.Run("submit first time or halfway through epoch", func(t *testing.T) {
		numberOfRequests := 2
		var wg sync.WaitGroup
		wg.Add(numberOfRequests) // Set up the wait group before starting goroutines

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparations(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, preparations []*eth2apiv1.ProposalPreparation) error {
			wg.Done()
			return nil
		}).Times(numberOfRequests)

		ticker := mocks.NewMockSlotTicker(ctrl)
		mockTimeChan := make(chan time.Time)
		mockSlotChan := make(chan phase0.Slot)
		ticker.EXPECT().Next().Return(mockTimeChan).AnyTimes()
		ticker.EXPECT().Slot().DoAndReturn(func() phase0.Slot {
			return <-mockSlotChan
		}).AnyTimes()

		frCtrl.beaconClient = client
		frCtrl.slotTickerProvider = func() slotticker.SlotTicker {
			return ticker
		}

		go frCtrl.Start(t.Context())

		slots := []phase0.Slot{
			1,  // first time
			2,  // should not call submit
			20, // should not call submit
			phase0.Slot(beaconConfig.SlotsPerEpoch / 2), // halfway through epoch
			63, // should not call submit
		}

		for _, s := range slots {
			mockTimeChan <- time.Now()
			mockSlotChan <- s
			time.Sleep(time.Millisecond * 500)
		}

		wg.Wait()

		close(mockTimeChan) // Close the channel after test
		close(mockSlotChan)
	})

	t.Run("error handling", func(t *testing.T) {
		var wg sync.WaitGroup
		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparations(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, preparations []*eth2apiv1.ProposalPreparation) error {
			wg.Done()
			return errors.New("failed to submit")
		}).Times(1)

		ticker := mocks.NewMockSlotTicker(ctrl)
		mockTimeChan := make(chan time.Time, 1)
		ticker.EXPECT().Next().Return(mockTimeChan).AnyTimes()
		ticker.EXPECT().Slot().Return(phase0.Slot(100)).AnyTimes()

		frCtrl.beaconClient = client
		frCtrl.slotTickerProvider = func() slotticker.SlotTicker {
			return ticker
		}

		go frCtrl.Start(t.Context())
		mockTimeChan <- time.Now()
		wg.Add(1)
		wg.Wait()
		close(mockTimeChan)
	})
}

func createStorage(t *testing.T) (basedb.Database, registrystorage.Shares, registrystorage.Recipients) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	shareStorage, _, err := registrystorage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	return db, shareStorage, registrystorage.NewRecipientsStorage(logger, db, []byte("test"))
}

func populateStorage(t *testing.T, storage registrystorage.Shares, operatorData *registrystorage.OperatorData) {
	createShare := func(index int, operatorID spectypes.OperatorID) *types.SSVShare {
		ownerAddr := fmt.Sprintf("%d", index)
		ownerAddrByte := [20]byte{}
		copy(ownerAddrByte[:], ownerAddr)

		return &types.SSVShare{
			Share: spectypes.Share{ValidatorPubKey: spectypes.ValidatorPK([]byte(fmt.Sprintf("pk%046d", index))),
				SharePubKey:    []byte(fmt.Sprintf("pk%046d", index)),
				ValidatorIndex: phase0.ValidatorIndex(index),
				Committee:      []*spectypes.ShareMember{{Signer: operatorID}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: common.BytesToAddress(ownerAddrByte[:]),
			Liquidated:   false,
		}
	}

	for i := 0; i < 1000; i++ {
		require.NoError(t, storage.Save(nil, createShare(i, operatorData.ID)))
	}

	// add non-committee share
	require.NoError(t, storage.Save(nil, createShare(2000, spectypes.OperatorID(1))))

	all := storage.List(nil, registrystorage.ByOperatorID(operatorData.ID), registrystorage.ByNotLiquidated())
	require.Equal(t, 1000, len(all))
}
