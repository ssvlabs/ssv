package fee_recipient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/operator/slotticker/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func TestSubmitProposal(t *testing.T) {
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	operatorData := &registrystorage.OperatorData{
		ID: 123456789,
	}

	operatorDataStore := operatordatastore.New(operatorData)

	db, shareStorage, recipientStorage := createStorage(t)
	defer db.Close()
	network := networkconfig.TestNetwork
	populateStorage(t, logger, shareStorage, operatorData)

	frCtrl := NewController(&ControllerOptions{
		Ctx:               context.TODO(),
		Network:           network,
		ShareStorage:      shareStorage,
		RecipientStorage:  recipientStorage,
		OperatorDataStore: operatorDataStore,
	})

	t.Run("submit first time or halfway through epoch", func(t *testing.T) {
		numberOfRequests := 4
		var wg sync.WaitGroup
		wg.Add(numberOfRequests) // Set up the wait group before starting goroutines

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any()).DoAndReturn(func(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
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

		go frCtrl.Start(logger)

		slots := []phase0.Slot{
			1,                                        // first time
			2,                                        // should not call submit
			20,                                       // should not call submit
			phase0.Slot(network.SlotsPerEpoch()) / 2, // halfway through epoch
			63,                                       // should not call submit
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
		client.EXPECT().SubmitProposalPreparation(gomock.Any()).DoAndReturn(func(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
			wg.Done()
			return errors.New("failed to submit")
		}).MinTimes(2).MaxTimes(2)

		ticker := mocks.NewMockSlotTicker(ctrl)
		mockTimeChan := make(chan time.Time, 1)
		ticker.EXPECT().Next().Return(mockTimeChan).AnyTimes()
		ticker.EXPECT().Slot().Return(phase0.Slot(100)).AnyTimes()

		frCtrl.beaconClient = client
		frCtrl.slotTickerProvider = func() slotticker.SlotTicker {
			return ticker
		}

		go frCtrl.Start(logger)
		mockTimeChan <- time.Now()
		wg.Add(2)
		wg.Wait()
		close(mockTimeChan)
	})
}

func createStorage(t *testing.T) (basedb.Database, registrystorage.Shares, registrystorage.Recipients) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	shareStorage, _, err := registrystorage.NewSharesStorage(networkconfig.TestNetwork, db, []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	return db, shareStorage, registrystorage.NewRecipientsStorage(logger, db, []byte("test"))
}

func populateStorage(t *testing.T, logger *zap.Logger, storage registrystorage.Shares, operatorData *registrystorage.OperatorData) {
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

	// add none committee share
	require.NoError(t, storage.Save(nil, createShare(2000, spectypes.OperatorID(1))))

	all := storage.List(nil, registrystorage.ByOperatorID(operatorData.ID), registrystorage.ByNotLiquidated())
	require.Equal(t, 1000, len(all))
}
