package fee_recipient

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/operator/slot_ticker/mocks"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"sync"
	"testing"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/golang/mock/gomock"
	prysmtypes "github.com/prysmaticlabs/eth2-types"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestSubmitProposal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	operatorKey := "123456789"

	_, collection := createStorage(t)
	//defer db.Close()
	network := beacon.NewNetwork(core.PraterNetwork, 0)
	populateStorage(t, collection, operatorKey)

	frCtrl := NewController(&ControllerOptions{
		Logger:            logex.GetLogger(),
		Ctx:               context.TODO(),
		EthNetwork:        network,
		ShareStorage:      collection,
		OperatorPublicKey: operatorKey,
	})

	t.Run("submit first time or first slot in epoch", func(t *testing.T) {
		numberOfRequests := 4
		var wg sync.WaitGroup
		client := beacon.NewMockBeacon(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any()).DoAndReturn(func(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
			wg.Done()
			return nil
		}).MinTimes(numberOfRequests).MaxTimes(numberOfRequests) // call first time and on the first slot of epoch. each time should be 2 request as we have two batches

		ticker := mocks.NewMockTicker(ctrl)
		ticker.EXPECT().Subscribe(gomock.Any()).DoAndReturn(func(subscription chan prysmtypes.Slot) event.Subscription {
			subscription <- 1 // first time
			time.Sleep(time.Millisecond * 500)
			subscription <- 2 // should not call submit
			time.Sleep(time.Millisecond * 500)
			subscription <- 20 // should not call submit
			time.Sleep(time.Millisecond * 500)
			subscription <- 32 // first slot of epoch
			return nil
		})

		frCtrl.beaconClient = client
		frCtrl.ticker = ticker

		go frCtrl.Start()
		wg.Add(numberOfRequests)
		wg.Wait()
	})

	t.Run("error handling", func(t *testing.T) {
		var wg sync.WaitGroup
		client := beacon.NewMockBeacon(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any()).DoAndReturn(func(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
			wg.Done()
			return errors.New("failed to submit")
		}).MinTimes(2).MaxTimes(2)

		ticker := mocks.NewMockTicker(ctrl)
		ticker.EXPECT().Subscribe(gomock.Any()).DoAndReturn(func(subscription chan prysmtypes.Slot) event.Subscription {
			subscription <- 100 // first time
			return nil
		})

		frCtrl.beaconClient = client
		frCtrl.ticker = ticker

		go frCtrl.Start()
		wg.Add(2)
		wg.Wait()
	})
}

func createStorage(t *testing.T) (basedb.IDb, validator.ICollection) {
	options := basedb.Options{
		Type:   "badger-memory",
		Logger: logex.GetLogger(),
		Path:   "",
	}

	db, err := storage.GetStorageFactory(options)
	require.NoError(t, err)

	return db, validator.NewCollection(validator.CollectionOptions{
		DB:     db,
		Logger: options.Logger,
	})
}

func populateStorage(t *testing.T, storage validator.ICollection, operatorKey string) {
	for i := 0; i < 1000; i++ {
		ownerAddr := fmt.Sprintf("%d", i)
		ownerAddrByte := [20]byte{}
		copy(ownerAddrByte[:], ownerAddr)

		share := &types.SSVShare{
			Share: spectypes.Share{ValidatorPubKey: []byte(fmt.Sprintf("pk%d", i))},
			Metadata: types.Metadata{
				BeaconMetadata: &beacon.ValidatorMetadata{
					Index: phase0.ValidatorIndex(i),
				},
				OwnerAddress: hex.EncodeToString(ownerAddrByte[:]),
				Operators:    [][]byte{[]byte(operatorKey)},
				Liquidated:   false,
			},
		}
		require.NoError(t, storage.SaveValidatorShare(share))
	}
	all, err := storage.GetFilteredValidatorShares(validator.NotLiquidatedAndByOperatorPubKey(operatorKey))
	require.NoError(t, err)
	require.Equal(t, 1000, len(all))
}
