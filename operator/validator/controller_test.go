package validator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ekm"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator/mocks"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/queue/worker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	sk2Str = "3748db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
)

// TODO: increase test coverage, add more tests, e.g.:
// 1. a validator with a non-empty share and empty metadata - test a scenario if we cannot get metadata from beacon node

type MockControllerOptions struct {
	network             P2PNetwork
	recipientsStorage   Recipients
	sharesStorage       SharesStorage
	metrics             validator.Metrics
	beacon              beacon.BeaconNode
	validatorOptions    validator.Options
	signer              spectypes.BeaconSigner
	metadataLastUpdated map[spectypes.ValidatorPK]time.Time
	StorageMap          *ibftstorage.QBFTStores
	validatorsMap       *validators.ValidatorsMap
	operatorDataStore   operatordatastore.OperatorDataStore
	operatorStorage     registrystorage.Operators
}

func TestNewController(t *testing.T) {
	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	_, logger, _, network, _, recipientStorage, bc := setupCommonTestComponents(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	registryStorage, newStorageErr := storage.NewNodeStorage(logger, db)
	require.NoError(t, newStorageErr)
	operatorSigner, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	controllerOptions := ControllerOptions{
		Beacon:            bc,
		Metrics:           nil,
		FullNode:          true,
		Network:           network,
		OperatorDataStore: operatorDataStore,
		OperatorSigner:    types.NewSsvOperatorSigner(operatorSigner, operatorDataStore.GetOperatorID),
		RegistryStorage:   registryStorage,
		RecipientsStorage: recipientStorage,
		Context:           context.Background(),
	}
	control := NewController(logger, controllerOptions)
	require.IsType(t, &controller{}, control)
}

func TestSetupValidatorsExporter(t *testing.T) {
	passedEpoch := phase0.Epoch(1)
	operators := buildOperators(t)

	operatorDataStore := operatordatastore.New(buildOperatorData(0, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")

	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	bcResponse := map[phase0.ValidatorIndex]*eth2apiv1.Validator{
		2: {
			Balance: 0,
			Status:  3,
			Index:   2,
			Validator: &phase0.Validator{
				ActivationEpoch: passedEpoch,
				PublicKey:       phase0.BLSPubKey(secretKey.GetPublicKey().Serialize()),
			},
		},
		3: {
			Balance: 0,
			Status:  3,
			Index:   3,
			Validator: &phase0.Validator{
				ActivationEpoch: passedEpoch,
				PublicKey:       phase0.BLSPubKey(secretKey2.GetPublicKey().Serialize()),
			},
		},
	}

	sharesWithMetadata := []*types.SSVShare{
		{
			Share: spectypes.Share{
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
			Metadata: types.Metadata{
				Liquidated: false,
				BeaconMetadata: &beacon.ValidatorMetadata{
					Balance:         0,
					Status:          3, // ValidatorStatePendingInitialized
					Index:           0,
					ActivationEpoch: passedEpoch,
				},
			},
		},
		{
			Share: spectypes.Share{
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey2.GetPublicKey().Serialize()),
			},
			Metadata: types.Metadata{
				Liquidated: false,
				BeaconMetadata: &beacon.ValidatorMetadata{
					Balance:         0,
					Status:          3, // Some other status
					Index:           0,
					ActivationEpoch: passedEpoch,
				},
			},
		},
	}
	_ = sharesWithMetadata

	sharesWithoutMetadata := []*types.SSVShare{
		{
			Share: spectypes.Share{
				//OperatorID:      1,
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
			Metadata: types.Metadata{
				Liquidated: false,
			},
		},
		{
			Share: spectypes.Share{
				//OperatorID:      2,
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey2.GetPublicKey().Serialize()),
			},
			Metadata: types.Metadata{
				Liquidated: false,
			},
		},
	}
	_ = sharesWithoutMetadata

	testCases := []struct {
		name                       string
		shareStorageListResponse   []*types.SSVShare
		expectMetadataFetch        bool
		syncHighestDecidedResponse error
		getValidatorDataResponse   error
	}{
		{"no shares of non committee", nil, false, nil, nil},
		{"set up non committee validators", sharesWithMetadata, false, nil, nil},
		{"set up non committee validators without metadata", sharesWithoutMetadata, true, nil, nil},
		{"fail to sync highest decided", sharesWithMetadata, false, errors.New("failed to sync highest decided"), nil},
		{"fail to update validators metadata", sharesWithMetadata, false, nil, errors.New("could not update all validators")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, logger, sharesStorage, network, _, recipientStorage, bc := setupCommonTestComponents(t)
			defer ctrl.Finish()
			mockValidatorsMap := validators.New(context.TODO())

			if tc.shareStorageListResponse == nil {
				sharesStorage.EXPECT().List(gomock.Any(), gomock.Any()).Return(tc.shareStorageListResponse).Times(1)
			} else {
				sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ basedb.Reader, pubKey []byte) *types.SSVShare {
					for _, share := range tc.shareStorageListResponse {
						if hex.EncodeToString(share.Share.ValidatorPubKey[:]) == hex.EncodeToString(pubKey) {
							return share
						}
					}
					return nil
				}).AnyTimes()
				sharesStorage.EXPECT().List(gomock.Any(), gomock.Any()).Return(tc.shareStorageListResponse).AnyTimes()
				sharesStorage.EXPECT().Range(gomock.Any(), gomock.Any()).DoAndReturn(func(_ basedb.Reader, fn func(*types.SSVShare) bool) {
					for _, share := range tc.shareStorageListResponse {
						if !fn(share) {
							break
						}
					}
				}).AnyTimes()
				if tc.expectMetadataFetch {
					bc.EXPECT().GetValidatorData(gomock.Any()).Return(bcResponse, tc.getValidatorDataResponse).Times(1)
					sharesStorage.EXPECT().UpdateValidatorMetadata(gomock.Any(), gomock.Any()).DoAndReturn(func(pk string, metadata *beacon.ValidatorMetadata) error {
						for _, share := range tc.shareStorageListResponse {
							if hex.EncodeToString(share.Share.ValidatorPubKey[:]) == pk {
								share.Metadata.BeaconMetadata = metadata
							}
						}
						return nil
					}).Times(len(tc.shareStorageListResponse))
					bc.EXPECT().GetBeaconNetwork().Return(networkconfig.Mainnet.Beacon.GetBeaconNetwork()).AnyTimes()
				}
				sharesStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(nil).AnyTimes()
				recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).Return(recipientData, true, nil).Times(0)
			}

			validatorStartFunc := func(validator *validator.Validator) (bool, error) {
				return true, nil
			}
			controllerOptions := MockControllerOptions{
				beacon:            bc,
				network:           network,
				operatorDataStore: operatorDataStore,
				sharesStorage:     sharesStorage,
				recipientsStorage: recipientStorage,
				validatorsMap:     mockValidatorsMap,
				validatorOptions: validator.Options{
					Exporter: true,
				},
				metrics:             validator.NopMetrics{},
				metadataLastUpdated: map[spectypes.ValidatorPK]time.Time{},
			}
			ctr := setupController(logger, controllerOptions)
			ctr.validatorStartFunc = validatorStartFunc
			ctr.StartValidators()
		})
	}
}

func TestHandleNonCommitteeMessages(t *testing.T) {
	logger := logging.TestLogger(t)
	mockValidatorsMap := validators.New(context.TODO())
	controllerOptions := MockControllerOptions{
		validatorsMap: mockValidatorsMap,
	}
	ctr := setupController(logger, controllerOptions) // none committee

	// Only exporter handles non committee messages
	ctr.validatorOptions.Exporter = true

	go ctr.handleRouterMessages()

	var wg sync.WaitGroup

	ctr.messageWorker.UseHandler(func(msg *queue.DecodedSSVMessage) error {
		wg.Done()
		return nil
	})

	wg.Add(2)

	identifier := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, []byte("pk"), spectypes.RoleCommittee)

	ctr.messageRouter.Route(context.TODO(), &queue.DecodedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{ // checks that not process unnecessary message
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   identifier,
			Data:    generateDecidedMessage(t, identifier),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.DecodedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{ // checks that not process unnecessary message
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   identifier,
			Data:    generateChangeRoundMsg(t, identifier),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.DecodedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{ // checks that not process unnecessary message
			MsgType: message.SSVSyncMsgType,
			MsgID:   identifier,
			Data:    []byte("data"),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.DecodedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   identifier,
			Data:    []byte("data"),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.DecodedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   identifier,
			Data:    []byte("data2"),
		},
	})

	go func() {
		time.Sleep(time.Second * 4)
		panic("time out!")
	}()

	wg.Wait()
}

func TestUpdateValidatorMetadata(t *testing.T) {

	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	passedEpoch := phase0.Epoch(1)
	validatorKey, err := createKey()
	require.NoError(t, err)

	operatorIds := []uint64{1, 2, 3, 4}
	operators := make([]*spectypes.ShareMember, len(operatorIds))
	for i, id := range operatorIds {
		operatorKey, keyError := createKey()
		require.NoError(t, keyError)
		operators[i] = &spectypes.ShareMember{Signer: id, SharePubKey: operatorKey}
	}

	firstValidator := &validator.Validator{
		DutyRunners: runner.ValidatorDutyRunners{},
		Storage:     ibftstorage.NewStores(),
		Share: &types.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
		},
	}
	shareWithMetaData := &types.SSVShare{
		Share: spectypes.Share{
			Committee:       operators,
			ValidatorPubKey: spectypes.ValidatorPK(validatorKey),
		},
		Metadata: types.Metadata{
			OwnerAddress: common.BytesToAddress([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			BeaconMetadata: &beacon.ValidatorMetadata{
				Balance:         0,
				Status:          3, // ValidatorStateActiveOngoing
				Index:           1,
				ActivationEpoch: passedEpoch,
			},
		},
	}

	validatorMetaData := &beacon.ValidatorMetadata{Index: 1, ActivationEpoch: passedEpoch, Status: eth2apiv1.ValidatorStateActiveOngoing}

	testCases := []struct {
		name                      string
		metadata                  *beacon.ValidatorMetadata
		ExpectedErrorResult       bool
		sharesStorageExpectReturn any
		getShareError             bool
		operatorDataId            uint64
		testPublicKey             spectypes.ValidatorPK
		mockRecipientTimes        int
	}{
		{"Empty metadata", nil, true, nil, false, 1, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()), 0},
		{"Valid metadata", validatorMetaData, false, nil, false, 1, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()), 0},
		{"Share wasn't found", validatorMetaData, true, nil, true, 1, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()), 0},
		{"Share not belong to operator", validatorMetaData, false, nil, false, 2, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()), 0},
		{"Metadata with error", validatorMetaData, true, fmt.Errorf("error"), false, 1, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()), 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize common setup
			ctrl, logger, sharesStorage, network, signer, recipientStorage, _ := setupCommonTestComponents(t)
			defer ctrl.Finish()
			operatorDataStore := operatordatastore.New(buildOperatorData(tc.operatorDataId, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
			recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")
			firstValidatorPublicKey := secretKey.GetPublicKey().Serialize()

			testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
				(spectypes.ValidatorPK)(firstValidatorPublicKey): firstValidator,
			}
			mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))

			// Assuming controllerOptions is set up correctly
			controllerOptions := MockControllerOptions{
				// keyManager:          km,
				signer:              signer,
				network:             network,
				operatorDataStore:   operatorDataStore,
				sharesStorage:       sharesStorage,
				recipientsStorage:   recipientStorage,
				validatorsMap:       mockValidatorsMap,
				metrics:             validator.NopMetrics{},
				metadataLastUpdated: map[spectypes.ValidatorPK]time.Time{},
			}

			if tc.getShareError {
				sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			} else {
				sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).Return(shareWithMetaData).AnyTimes()
			}
			recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).Return(recipientData, true, nil).Times(tc.mockRecipientTimes)
			sharesStorage.EXPECT().UpdateValidatorMetadata(gomock.Any(), gomock.Any()).Return(tc.sharesStorageExpectReturn).AnyTimes()

			ctr := setupController(logger, controllerOptions)

			validatorStartFunc := func(validator *validator.Validator) (bool, error) {
				return true, nil
			}
			ctr.validatorStartFunc = validatorStartFunc
			err := ctr.UpdateValidatorMetadata(tc.testPublicKey, tc.metadata)

			if tc.ExpectedErrorResult {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSetupValidators(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)

	// Init global variables
	passedEpoch := phase0.Epoch(1)
	operatorIds := []uint64{1, 2, 3, 4}
	var validatorPublicKey phase0.BLSPubKey

	validatorKey, err := createKey()
	require.NoError(t, err)

	// Check the length before copying
	if len(validatorKey) == len(validatorPublicKey) {
		copy(validatorPublicKey[:], validatorKey)
	} else {
		t.Fatalf("Length mismatch: validatorKey has length %d, but expected %d", len(validatorKey), len(validatorPublicKey))
	}

	metadataLastMap := make(map[spectypes.ValidatorPK]time.Time)
	metadataLastMap[spectypes.ValidatorPK(validatorPublicKey)] = time.Now()

	operators := make([]*spectypes.ShareMember, len(operatorIds))
	for i, id := range operatorIds {
		operatorKey, keyError := createKey()
		require.NoError(t, keyError)
		operators[i] = &spectypes.ShareMember{Signer: id, SharePubKey: operatorKey}
	}

	shareWithMetaData := &types.SSVShare{
		Share: spectypes.Share{
			// OperatorID:      2,
			Committee:       operators[:1],
			ValidatorPubKey: spectypes.ValidatorPK(validatorKey),
		},
		Metadata: types.Metadata{
			OwnerAddress: common.BytesToAddress([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			BeaconMetadata: &beacon.ValidatorMetadata{
				Balance:         0,
				Status:          3, // ValidatorStateActiveOngoing
				Index:           1,
				ActivationEpoch: passedEpoch,
			},
		},
	}

	shareWithoutMetaData := &types.SSVShare{
		Share: spectypes.Share{
			// OperatorID:      2,
			Committee:       operators[:1],
			ValidatorPubKey: spectypes.ValidatorPK(validatorKey),
		},
		Metadata: types.Metadata{
			OwnerAddress:   common.BytesToAddress([]byte("62Ce5c69260bd819B4e0AD13f4b873074D479811")),
			BeaconMetadata: nil,
		},
	}

	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")
	ownerAddressBytes := decodeHex(t, "67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	feeRecipientBytes := decodeHex(t, "45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")
	testValidator := setupTestValidator(ownerAddressBytes, feeRecipientBytes)
	storageMu := sync.Mutex{}
	storageData := make(map[string]*beacon.ValidatorMetadata)

	opStorage, done := newOperatorStorageForTest(logger)
	defer done()

	opStorage.SaveOperatorData(nil, buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))

	bcResponse := map[phase0.ValidatorIndex]*eth2apiv1.Validator{
		0: {
			Balance: 0,
			Status:  3,
			Index:   2,
			Validator: &phase0.Validator{
				ActivationEpoch: passedEpoch,
				PublicKey:       validatorPublicKey,
			},
		},
	}

	testCases := []struct {
		recipientMockTimes int
		bcMockTimes        int
		recipientFound     bool
		inited             int
		started            int
		recipientErr       error
		name               string
		shares             []*types.SSVShare
		recipientData      *registrystorage.RecipientData
		bcResponse         map[phase0.ValidatorIndex]*eth2apiv1.Validator
		validatorStartFunc func(validator *validator.Validator) (bool, error)
	}{
		{
			name:               "setting fee recipient to storage data",
			shares:             []*types.SSVShare{shareWithMetaData, shareWithoutMetaData},
			recipientData:      recipientData,
			recipientFound:     true,
			recipientErr:       nil,
			recipientMockTimes: 1,
			bcResponse:         bcResponse,
			inited:             1,
			started:            1,
			bcMockTimes:        1,
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, nil
			},
		},
		{
			name:               "setting fee recipient to owner address",
			shares:             []*types.SSVShare{shareWithMetaData, shareWithoutMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       nil,
			recipientMockTimes: 1,
			bcResponse:         bcResponse,
			inited:             1,
			started:            1,
			bcMockTimes:        1,
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, nil
			},
		},
		{
			name:               "failed to set fee recipient",
			shares:             []*types.SSVShare{shareWithMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       errors.New("some error"),
			recipientMockTimes: 1,
			bcResponse:         bcResponse,
			inited:             0,
			started:            0,
			bcMockTimes:        0,
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, nil
			},
		},
		{
			name:               "start share with metadata",
			shares:             []*types.SSVShare{shareWithMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       nil,
			recipientMockTimes: 1,
			bcResponse:         bcResponse,
			inited:             1,
			started:            1,
			bcMockTimes:        0,
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, nil
			},
		},
		{
			name:               "start share without metadata",
			bcMockTimes:        1,
			inited:             0,
			started:            0,
			recipientMockTimes: 0,
			recipientData:      nil,
			recipientErr:       nil,
			recipientFound:     false,
			bcResponse:         bcResponse,
			shares:             []*types.SSVShare{shareWithoutMetaData},
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, nil
			},
		},
		{
			name:               "failed to get GetValidatorData",
			recipientMockTimes: 0,
			bcMockTimes:        1,
			recipientData:      nil,
			recipientErr:       nil,
			bcResponse:         nil,
			recipientFound:     false,
			inited:             0,
			started:            0,
			shares:             []*types.SSVShare{shareWithoutMetaData},
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, nil
			},
		},
		{
			name:               "failed to start validator",
			shares:             []*types.SSVShare{shareWithMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       nil,
			recipientMockTimes: 1,
			bcResponse:         bcResponse,
			inited:             1,
			started:            0,
			bcMockTimes:        0,
			validatorStartFunc: func(validator *validator.Validator) (bool, error) {
				return true, errors.New("some error")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			bc := beacon.NewMockBeaconNode(ctrl)
			storageMap := ibftstorage.NewStores()
			network := mocks.NewMockP2PNetwork(ctrl)
			recipientStorage := mocks.NewMockRecipients(ctrl)
			sharesStorage := mocks.NewMockSharesStorage(ctrl)
			sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).Return(shareWithMetaData).AnyTimes()
			sharesStorage.EXPECT().UpdateValidatorMetadata(gomock.Any(), gomock.Any()).DoAndReturn(func(pk string, metadata *beacon.ValidatorMetadata) error {
				storageMu.Lock()
				defer storageMu.Unlock()

				storageData[pk] = metadata

				return nil
			}).AnyTimes()

			testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
				createPubKey(byte('0')): testValidator,
			}
			committeMap := make(map[spectypes.CommitteeID]*validator.Committee)
			mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, committeMap))

			bc.EXPECT().GetBeaconNetwork().Return(networkconfig.TestNetwork.Beacon.GetBeaconNetwork()).AnyTimes()

			// Set up the controller with mock data
			controllerOptions := MockControllerOptions{
				beacon:            bc,
				network:           network,
				sharesStorage:     sharesStorage,
				operatorDataStore: operatorDataStore,
				recipientsStorage: recipientStorage,
				operatorStorage:   opStorage,
				validatorsMap:     mockValidatorsMap,
				validatorOptions: validator.Options{
					NetworkConfig: networkconfig.TestNetwork,
					Storage:       storageMap,
				},
				metadataLastUpdated: metadataLastMap,
				metrics:             validator.NopMetrics{},
			}

			recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).Return(tc.recipientData, tc.recipientFound, tc.recipientErr).Times(tc.recipientMockTimes)
			ctr := setupController(logger, controllerOptions)
			ctr.validatorStartFunc = tc.validatorStartFunc
			inited, _ := ctr.setupValidators(tc.shares)
			require.Len(t, inited, tc.inited)
			// TODO: Alan, should we check for committee too?
			started := ctr.startValidators(inited, nil)
			require.Equal(t, tc.started, started)

			//Add any assertions here to validate the behavior
		})
	}
}

func TestGetValidator(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)

	// Initialize a test validator with the decoded owner address
	testValidator := &validator.Validator{
		Share: &types.SSVShare{
			Metadata: types.Metadata{},
		},
	}

	testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
		createPubKey(byte('0')): testValidator,
	}
	mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))
	// Set up the controller with mock data
	controllerOptions := MockControllerOptions{
		validatorsMap: mockValidatorsMap,
	}
	ctr := setupController(logger, controllerOptions)

	// Execute the function under test and validate results
	_, found := ctr.GetValidator(createPubKey(byte('0')))
	require.True(t, found)
	_, found = ctr.GetValidator(createPubKey(byte('1')))
	require.False(t, found)
}

func TestGetValidatorStats(t *testing.T) {
	// Common setup
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	sharesStorage := mocks.NewMockSharesStorage(ctrl)
	bc := beacon.NewMockBeaconNode(ctrl)
	passedEpoch := phase0.Epoch(1)

	netCfg := networkconfig.TestNetwork
	bc.EXPECT().GetBeaconNetwork().Return(netCfg.Beacon.GetBeaconNetwork()).AnyTimes()

	t.Run("Test with multiple operators", func(t *testing.T) {
		// Setup for this subtest
		operatorIds := []uint64{1, 2, 3}
		operators := make([]*spectypes.ShareMember, len(operatorIds))
		for i, id := range operatorIds {
			operators[i] = &spectypes.ShareMember{Signer: id}
		}

		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					Committee: operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
			{
				Share: spectypes.Share{
					Committee: operators[1:],
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          1, // Some other status
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage:     sharesStorage,
			validatorsMap:     validators.New(context.TODO()),
			operatorDataStore: operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			beacon:            bc,
		}

		ctr := setupController(logger, controllerOptions)

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 1, int(operatorShares), "Unexpected operator shares count")
	})

	t.Run("Test with single operator", func(t *testing.T) {
		// Setup for this subtest
		operatorIds := []uint64{1}
		operators := make([]*spectypes.ShareMember, len(operatorIds))
		for i, id := range operatorIds {
			operators[i] = &spectypes.ShareMember{Signer: id}
		}

		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					Committee: operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage:     sharesStorage,
			validatorsMap:     validators.New(context.TODO()),
			operatorDataStore: operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			beacon:            bc,
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 1, int(operatorShares), "Unexpected operator shares count")
	})

	t.Run("Test with no operators", func(t *testing.T) {
		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					Committee: nil,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage:     sharesStorage,
			validatorsMap:     validators.New(context.TODO()),
			operatorDataStore: operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			beacon:            bc,
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 0, int(operatorShares), "Unexpected operator shares count")
	})

	t.Run("Test with varying statuses", func(t *testing.T) {
		// Setup for this subtest
		operatorIds := []uint64{1}
		operators := make([]*spectypes.ShareMember, len(operatorIds))
		for i, id := range operatorIds {
			operators[i] = &spectypes.ShareMember{Signer: id}
		}

		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					Committee: operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
			{
				Share: spectypes.Share{
					Committee: operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          1,
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage:     sharesStorage,
			validatorsMap:     validators.New(context.TODO()),
			operatorDataStore: operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			beacon:            bc,
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 2, int(operatorShares), "Unexpected operator shares count")
	})
}

func TestUpdateFeeRecipient(t *testing.T) {
	// Setup logger for testing
	logger := logging.TestLogger(t)

	ownerAddressBytes := decodeHex(t, "67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	fakeOwnerAddressBytes := decodeHex(t, "61Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode fake owner address")
	firstFeeRecipientBytes := decodeHex(t, "41E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode first fee recipient address")
	secondFeeRecipientBytes := decodeHex(t, "45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")

	t.Run("Test with right owner address", func(t *testing.T) {
		testValidator := setupTestValidator(ownerAddressBytes, firstFeeRecipientBytes)

		testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
			createPubKey(byte('0')): testValidator,
		}
		mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))

		controllerOptions := MockControllerOptions{validatorsMap: mockValidatorsMap}
		ctr := setupController(logger, controllerOptions)

		err := ctr.UpdateFeeRecipient(common.BytesToAddress(ownerAddressBytes), common.BytesToAddress(secondFeeRecipientBytes))
		require.NoError(t, err, "Unexpected error while updating fee recipient with correct owner address")

		actualFeeRecipient := testValidator.Share.FeeRecipientAddress[:]
		require.Equal(t, secondFeeRecipientBytes, actualFeeRecipient, "Fee recipient address did not update correctly")
	})

	t.Run("Test with wrong owner address", func(t *testing.T) {
		testValidator := setupTestValidator(ownerAddressBytes, firstFeeRecipientBytes)
		testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
			createPubKey(byte('0')): testValidator,
		}
		mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))
		controllerOptions := MockControllerOptions{validatorsMap: mockValidatorsMap}
		ctr := setupController(logger, controllerOptions)

		err := ctr.UpdateFeeRecipient(common.BytesToAddress(fakeOwnerAddressBytes), common.BytesToAddress(secondFeeRecipientBytes))
		require.NoError(t, err, "Unexpected error while updating fee recipient with incorrect owner address")

		actualFeeRecipient := testValidator.Share.FeeRecipientAddress[:]
		require.Equal(t, firstFeeRecipientBytes, actualFeeRecipient, "Fee recipient address should not have changed")
	})
}

func TestGetIndices(t *testing.T) {
	farFutureEpoch := phase0.Epoch(99999)
	currentEpoch := phase0.Epoch(100)
	testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
		createPubKey(byte('0')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          0, // ValidatorStateUnknown
			Index:           0,
			ActivationEpoch: farFutureEpoch,
		}),
		createPubKey(byte('1')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          1, // ValidatorStatePendingInitialized
			Index:           0,
			ActivationEpoch: farFutureEpoch,
		}),
		createPubKey(byte('2')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          2, // ValidatorStatePendingQueued
			Index:           3,
			ActivationEpoch: phase0.Epoch(101),
		}),

		createPubKey(byte('3')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          3, // ValidatorStateActiveOngoing
			Index:           3,
			ActivationEpoch: phase0.Epoch(100),
		}),
		createPubKey(byte('4')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          4, // ValidatorStateActiveExiting
			Index:           4,
			ActivationEpoch: phase0.Epoch(100),
		}),
		createPubKey(byte('5')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          5, // ValidatorStateActiveSlashed
			Index:           5,
			ActivationEpoch: phase0.Epoch(100),
		}),

		createPubKey(byte('6')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          6, // ValidatorStateExitedUnslashed
			Index:           6,
			ActivationEpoch: phase0.Epoch(100),
		}),
		createPubKey(byte('7')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          7, // ValidatorStateExitedSlashed
			Index:           7,
			ActivationEpoch: phase0.Epoch(100),
		}),
		createPubKey(byte('8')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          8, // ValidatorStateWithdrawalPossible
			Index:           8,
			ActivationEpoch: phase0.Epoch(100),
		}),
		createPubKey(byte('9')): newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          9, // ValidatorStateWithdrawalDone
			Index:           9,
			ActivationEpoch: phase0.Epoch(100),
		}),
	}
	mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))
	logger := logging.TestLogger(t)
	controllerOptions := MockControllerOptions{
		validatorsMap: mockValidatorsMap,
	}
	ctr := setupController(logger, controllerOptions)

	activeIndicesForCurrentEpoch := ctr.CommitteeActiveIndices(currentEpoch)
	require.Equal(t, 2, len(activeIndicesForCurrentEpoch)) // should return only active indices

	activeIndicesForNextEpoch := ctr.CommitteeActiveIndices(currentEpoch + 1)
	require.Equal(t, 3, len(activeIndicesForNextEpoch)) // should return including ValidatorStatePendingQueued
}

func setupController(logger *zap.Logger, opts MockControllerOptions) controller {
	return controller{
		metadataUpdateInterval:  0,
		logger:                  logger,
		beacon:                  opts.beacon,
		network:                 opts.network,
		metrics:                 opts.metrics,
		ibftStorageMap:          opts.StorageMap,
		operatorDataStore:       opts.operatorDataStore,
		sharesStorage:           opts.sharesStorage,
		operatorsStorage:        opts.operatorStorage,
		validatorsMap:           opts.validatorsMap,
		context:                 context.Background(),
		validatorOptions:        opts.validatorOptions,
		recipientsStorage:       opts.recipientsStorage,
		messageRouter:           newMessageRouter(logger),
		committeeValidatorSetup: make(chan struct{}),
		indicesChange:           make(chan struct{}, 32),
		messageWorker: worker.NewWorker(logger, &worker.Config{
			Ctx:          context.Background(),
			WorkersCount: 1,
			Buffer:       100,
		}),
		metadataLastUpdated: opts.metadataLastUpdated,
	}
}

func newValidator(metaData *beacon.ValidatorMetadata) *validator.Validator {
	return &validator.Validator{
		Share: &types.SSVShare{
			Metadata: types.Metadata{
				BeaconMetadata: metaData,
			},
		},
	}
}

func generateChangeRoundMsg(t *testing.T, identifier spectypes.MessageID) []byte {
	msg := specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     0,
		Round:      1,
		Identifier: identifier[:],
		Root:       [32]byte{1, 2, 3},
	}

	msgEncoded, err := msg.Encode()
	if err != nil {
		panic(err)
	}
	sig := append([]byte{1, 2, 3, 4}, make([]byte, 92)...)
	sm := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID(msg.Identifier),
			Data:    msgEncoded,
		},
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  append(make([][]byte, 0), sig),
		OperatorIDs: []spectypes.OperatorID{1, 2, 3},
	}
	res, err := sm.Encode()
	require.NoError(t, err)
	return res
}

func generateDecidedMessage(t *testing.T, identifier spectypes.MessageID) []byte {
	// sm := specqbft.SignedMessage{
	// 	Signature: append([]byte{1, 2, 3, 4}, make([]byte, 92)...),
	// 	Signers:   []spectypes.OperatorID{1, 2, 3},
	// 	Message: specqbft.Message{
	// 		MsgType:    specqbft.CommitMsgType,
	// 		Height:     0,
	// 		Round:      1,
	// 		Identifier: identifier[:],
	// 		Root:       [32]byte{1, 2, 3},
	// 	},
	// }

	msg := specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     0,
		Round:      1,
		Identifier: identifier[:],
		Root:       [32]byte{1, 2, 3},
	}
	msgEncoded, err := msg.Encode()
	if err != nil {
		panic(err)
	}
	sig := append([]byte{1, 2, 3, 4}, make([]byte, 92)...)
	sm := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID(msg.Identifier),
			Data:    msgEncoded,
		},
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  append(make([][]byte, 0), sig),
		OperatorIDs: []spectypes.OperatorID{1, 2, 3},
	}
	res, err := sm.Encode()
	require.NoError(t, err)
	return res
}

func setupTestValidator(ownerAddressBytes, feeRecipientBytes []byte) *validator.Validator {
	return &validator.Validator{
		DutyRunners: runner.ValidatorDutyRunners{},
		Storage:     ibftstorage.NewStores(),
		Share: &types.SSVShare{
			Share: spectypes.Share{
				FeeRecipientAddress: common.BytesToAddress(feeRecipientBytes),
			},
			Metadata: types.Metadata{
				OwnerAddress: common.BytesToAddress(ownerAddressBytes),
			},
		},
	}
}

func getBaseStorage(logger *zap.Logger) (basedb.Database, error) {
	return kv.NewInMemory(logger, basedb.Options{})
}

func decodeHex(t *testing.T, hexStr string, errMsg string) []byte {
	bytes, err := hex.DecodeString(hexStr)
	require.NoError(t, err, errMsg)
	return bytes
}

func buildOperatorData(id uint64, ownerAddress string) *registrystorage.OperatorData {
	return &registrystorage.OperatorData{
		ID:           id,
		PublicKey:    []byte("samplePublicKey"),
		OwnerAddress: common.BytesToAddress([]byte(ownerAddress)),
	}
}

func buildFeeRecipient(Owner string, FeeRecipient string) *registrystorage.RecipientData {
	feeRecipientSlice := []byte(FeeRecipient) // Assuming FeeRecipient is a string or similar
	var executionAddress bellatrix.ExecutionAddress
	copy(executionAddress[:], feeRecipientSlice)
	return &registrystorage.RecipientData{
		Owner:        common.BytesToAddress([]byte(Owner)),
		FeeRecipient: executionAddress,
		Nonce:        nil,
	}
}

func createKey() ([]byte, error) {
	pubKey := make([]byte, 48)
	_, err := rand.Read(pubKey)
	if err != nil {
		fmt.Println("Error generating random bytes:", err)
		return nil, err
	}
	return pubKey, nil
}

func setupCommonTestComponents(t *testing.T) (*gomock.Controller, *zap.Logger, *mocks.MockSharesStorage, *mocks.MockP2PNetwork, ekm.KeyManager, *mocks.MockRecipients, *beacon.MockBeaconNode) {
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	bc := beacon.NewMockBeaconNode(ctrl)
	network := mocks.NewMockP2PNetwork(ctrl)
	sharesStorage := mocks.NewMockSharesStorage(ctrl)
	recipientStorage := mocks.NewMockRecipients(ctrl)

	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	km, err := ekm.NewETHKeyManagerSigner(logger, db, networkconfig.TestNetwork, "")
	require.NoError(t, err)
	return ctrl, logger, sharesStorage, network, km, recipientStorage, bc
}

func buildOperators(t *testing.T) []*spectypes.ShareMember {
	operatorIds := []uint64{1, 2, 3, 4}
	operators := make([]*spectypes.ShareMember, len(operatorIds))
	for i, id := range operatorIds {
		operatorKey, keyError := createKey()
		require.NoError(t, keyError)
		operators[i] = &spectypes.ShareMember{Signer: id, SharePubKey: operatorKey}
	}
	return operators
}

func createPubKey(input byte) spectypes.ValidatorPK {
	var byteArray [48]byte
	for i := range byteArray {
		byteArray[i] = input
	}
	return byteArray
}

func newOperatorStorageForTest(logger *zap.Logger) (registrystorage.Operators, func()) {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, func() {}
	}
	s := registrystorage.NewOperatorsStorage(logger, db, []byte("test"))
	return s, func() {
		db.Close()
	}
}
