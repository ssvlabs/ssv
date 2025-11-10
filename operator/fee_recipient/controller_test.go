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

// testRecipientStorage: in-memory overrides for owner->custom recipient
type testRecipientStorage struct {
	customRecipients map[common.Address]bellatrix.ExecutionAddress
}

func newTestRecipientStorage() *testRecipientStorage {
	return &testRecipientStorage{
		customRecipients: make(map[common.Address]bellatrix.ExecutionAddress),
	}
}

func (trs *testRecipientStorage) SetCustomRecipient(owner common.Address, recipient bellatrix.ExecutionAddress) {
	trs.customRecipients[owner] = recipient
}

func (trs *testRecipientStorage) GetFeeRecipient(owner common.Address) (bellatrix.ExecutionAddress, error) {
	if recipient, ok := trs.customRecipients[owner]; ok {
		return recipient, nil
	}
	return bellatrix.ExecutionAddress{}, fmt.Errorf("no custom recipient for owner %s", owner.Hex())
}

// testValidatorProvider implements ValidatorProvider using registry storage and optional overrides.
type testValidatorProvider struct {
	shares           registrystorage.Shares
	recipientStorage *testRecipientStorage
	operatorID       spectypes.OperatorID
}

func newTestValidatorProvider(shares registrystorage.Shares, recipients *testRecipientStorage, operatorID spectypes.OperatorID) *testValidatorProvider {
	return &testValidatorProvider{
		shares:           shares,
		recipientStorage: recipients,
		operatorID:       operatorID,
	}
}

func (tvp *testValidatorProvider) SelfValidators() []*types.SSVShare {
	return tvp.shares.List(nil, registrystorage.ByOperatorID(tvp.operatorID), registrystorage.ByNotLiquidated())
}

func (tvp *testValidatorProvider) GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	// Find the share to obtain its owner
	shares := tvp.shares.List(nil)
	for _, share := range shares {
		if share.ValidatorPubKey == validatorPK {
			// 1) custom override
			if tvp.recipientStorage != nil {
				if rec, err := tvp.recipientStorage.GetFeeRecipient(share.OwnerAddress); err == nil {
					return rec, nil
				}
			}
			// 2) default to owner address bytes
			return bellatrix.ExecutionAddress(share.OwnerAddress), nil
		}
	}
	return bellatrix.ExecutionAddress{}, fmt.Errorf("validator not found: %x", validatorPK)
}

func TestSubmitProposal(t *testing.T) {
	logger := log.TestLogger(t)
	ctrl := gomock.NewController(t)

	operatorData := &registrystorage.OperatorData{ID: 123456789}
	operatorDataStore := operatordatastore.New(operatorData)

	db, shareStorage := createStorage(t)
	defer func() { require.NoError(t, db.Close()) }()

	beaconConfig := networkconfig.TestNetwork.Beacon
	populateStorage(t, shareStorage, operatorData)

	t.Run("submit first time or halfway through epoch", func(t *testing.T) {
		validatorProvider := newTestValidatorProvider(shareStorage, nil, operatorData.ID)
		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})

		var wg sync.WaitGroup
		wg.Add(2) // expect 2 submits: first tick + mid-epoch tick

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().
			SubmitProposalPreparations(gomock.Any(), gomock.AssignableToTypeOf([]*eth2apiv1.ProposalPreparation{})).
			DoAndReturn(func(ctx context.Context, preparations []*eth2apiv1.ProposalPreparation) error {
				wg.Done()
				return nil
			}).
			Times(2)

		// Mock slot ticker: first tick always submits, then only at mid-epoch
		ticker := mocks.NewMockSlotTicker(ctrl)
		mockTimeChan := make(chan time.Time)
		mockSlotChan := make(chan phase0.Slot)
		ticker.EXPECT().Next().Return(mockTimeChan).AnyTimes()
		ticker.EXPECT().Slot().DoAndReturn(func() phase0.Slot { return <-mockSlotChan }).AnyTimes()

		frCtrl.beaconClient = client
		frCtrl.slotTickerProvider = func() slotticker.SlotTicker { return ticker }

		go frCtrl.Start(t.Context())

		slots := []phase0.Slot{
			1,  // first time (always submit)
			2,  // ignore
			20, // ignore
			phase0.Slot(beaconConfig.SlotsPerEpoch / 2), // mid-epoch -> submit
			63, // ignore
		}

		for _, s := range slots {
			mockTimeChan <- time.Now()
			mockSlotChan <- s
			time.Sleep(50 * time.Millisecond)
		}

		wg.Wait()
		close(mockTimeChan)
		close(mockSlotChan)
	})

	t.Run("error handling (SubmitProposalPreparations returns error)", func(t *testing.T) {
		validatorProvider := newTestValidatorProvider(shareStorage, nil, operatorData.ID)
		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})

		var wg sync.WaitGroup
		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().
			SubmitProposalPreparations(gomock.Any(), gomock.AssignableToTypeOf([]*eth2apiv1.ProposalPreparation{})).
			DoAndReturn(func(ctx context.Context, _ []*eth2apiv1.ProposalPreparation) error {
				wg.Done()
				return errors.New("failed to submit")
			}).
			Times(1)

		ticker := mocks.NewMockSlotTicker(ctrl)
		mockTimeChan := make(chan time.Time, 1)
		ticker.EXPECT().Next().Return(mockTimeChan).AnyTimes()
		ticker.EXPECT().Slot().Return(phase0.Slot(100)).AnyTimes()

		frCtrl.beaconClient = client
		frCtrl.slotTickerProvider = func() slotticker.SlotTicker { return ticker }

		go frCtrl.Start(t.Context())
		wg.Add(1)
		mockTimeChan <- time.Now()
		wg.Wait()
		close(mockTimeChan)
	})

	t.Run("custom fee recipients from storage", func(t *testing.T) {
		// recipient overrides for two owners (validator indices 0 and 1)
		testRecipients := newTestRecipientStorage()
		overrideA := bellatrix.ExecutionAddress{0x11, 0x22, 0x33, 0x44, 0x55}
		overrideB := bellatrix.ExecutionAddress{0xaa, 0xbb, 0xcc, 0xdd, 0xee}
		testRecipients.SetCustomRecipient(common.HexToAddress("0x0000000000000000000000000000000000000000"), overrideA)
		testRecipients.SetCustomRecipient(common.HexToAddress("0x0000000000000000000000000000000000000001"), overrideB)

		validatorProvider := newTestValidatorProvider(shareStorage, testRecipients, operatorData.ID)
		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})

		var captured []*eth2apiv1.ProposalPreparation
		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().
			SubmitProposalPreparations(gomock.Any(), gomock.AssignableToTypeOf([]*eth2apiv1.ProposalPreparation{})).
			DoAndReturn(func(ctx context.Context, pp []*eth2apiv1.ProposalPreparation) error {
				captured = pp
				return nil
			}).
			Times(1)

		frCtrl.beaconClient = client
		require.NoError(t, frCtrl.prepareAndSubmit(t.Context()))

		// Build map for easy assertions
		got := map[phase0.ValidatorIndex]bellatrix.ExecutionAddress{}
		for _, p := range captured {
			got[p.ValidatorIndex] = p.FeeRecipient
		}

		require.Equal(t, overrideA, got[0], "index 0 should use custom recipient")
		require.Equal(t, overrideB, got[1], "index 1 should use custom recipient")

		// index 2 should fall back to owner address
		owner2 := common.HexToAddress("0x0000000000000000000000000000000000000002")
		expected := bellatrix.ExecutionAddress(owner2)
		require.Equal(t, expected, got[2], "index 2 should use owner address as default")
	})

	t.Run("correct validator index mapping and exclusion of non-committee", func(t *testing.T) {
		validatorProvider := newTestValidatorProvider(shareStorage, nil, operatorData.ID)
		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})

		var captured []*eth2apiv1.ProposalPreparation
		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().
			SubmitProposalPreparations(gomock.Any(), gomock.AssignableToTypeOf([]*eth2apiv1.ProposalPreparation{})).
			DoAndReturn(func(ctx context.Context, pp []*eth2apiv1.ProposalPreparation) error {
				captured = pp
				return nil
			}).
			Times(1)

		frCtrl.beaconClient = client
		require.NoError(t, frCtrl.prepareAndSubmit(t.Context()))

		require.Len(t, captured, 100, "should include exactly 100 committee validators")
		indexSet := make(map[phase0.ValidatorIndex]struct{}, len(captured))
		for _, p := range captured {
			indexSet[p.ValidatorIndex] = struct{}{}
		}
		for i := 0; i < 100; i++ {
			_, ok := indexSet[phase0.ValidatorIndex(i)]
			require.True(t, ok, "validator index %d should be present", i)
		}
		_, present := indexSet[phase0.ValidatorIndex(2000)]
		require.False(t, present, "non-committee validator must not be included")
	})

	t.Run("500 preparations created (no batching at controller level)", func(t *testing.T) {
		// fresh storage with exactly 500 committee validators
		db2, shareStorage2 := createStorage(t)
		defer func() { require.NoError(t, db2.Close()) }()

		for i := 0; i < 500; i++ {
			owner := common.HexToAddress(fmt.Sprintf("0x%040x", i))
			share := &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: spectypes.ValidatorPK([]byte(fmt.Sprintf("pk%046d", i))),
					SharePubKey:     []byte(fmt.Sprintf("pk%046d", i)),
					ValidatorIndex:  phase0.ValidatorIndex(i),
					Committee:       []*spectypes.ShareMember{{Signer: operatorData.ID}},
				},
				Status:       eth2apiv1.ValidatorStateActiveOngoing,
				OwnerAddress: owner,
				Liquidated:   false,
			}
			require.NoError(t, shareStorage2.Save(nil, share))
		}

		validatorProvider := newTestValidatorProvider(shareStorage2, nil, operatorData.ID)
		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})

		count := 0
		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().
			SubmitProposalPreparations(gomock.Any(), gomock.AssignableToTypeOf([]*eth2apiv1.ProposalPreparation{})).
			DoAndReturn(func(ctx context.Context, pp []*eth2apiv1.ProposalPreparation) error {
				count = len(pp)
				return nil
			}).
			Times(1)

		frCtrl.beaconClient = client
		require.NoError(t, frCtrl.prepareAndSubmit(t.Context()))
		require.Equal(t, 500, count, "controller should submit all 500 preparations in one call")
	})
}

func createStorage(t *testing.T) (basedb.Database, registrystorage.Shares) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	// Minimal recipients storage just to satisfy SharesStorage init (if needed)
	recipientStorage, err := registrystorage.NewRecipientsStorage(logger, db, []byte("test"))
	require.NoError(t, err)

	shareStorage, _, err := registrystorage.NewSharesStorage(
		networkconfig.TestNetwork.Beacon,
		db,
		recipientStorage.GetFeeRecipient, // pass fee-recipient resolver hook if required by ctor
		[]byte("test"),
	)
	require.NoError(t, err)

	return db, shareStorage
}

func populateStorage(t *testing.T, storage registrystorage.Shares, operatorData *registrystorage.OperatorData) {
	createShare := func(index int, operatorID spectypes.OperatorID) *types.SSVShare {
		ownerAddr := common.HexToAddress(fmt.Sprintf("0x%040x", index))
		return &types.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK([]byte(fmt.Sprintf("pk%046d", index))),
				SharePubKey:     []byte(fmt.Sprintf("pk%046d", index)),
				ValidatorIndex:  phase0.ValidatorIndex(index),
				Committee:       []*spectypes.ShareMember{{Signer: operatorID}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: ownerAddr,
			Liquidated:   false,
		}
	}

	for i := 0; i < 100; i++ {
		require.NoError(t, storage.Save(nil, createShare(i, operatorData.ID)))
	}

	// non-committee share
	require.NoError(t, storage.Save(nil, createShare(2000, spectypes.OperatorID(1))))

	all := storage.List(nil, registrystorage.ByOperatorID(operatorData.ID), registrystorage.ByNotLiquidated())
	require.Equal(t, 100, len(all))
}
