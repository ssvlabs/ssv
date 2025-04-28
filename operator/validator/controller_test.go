package validator

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
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
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
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
	network           P2PNetwork
	recipientsStorage Recipients
	sharesStorage     SharesStorage
	beacon            beacon.BeaconNode
	validatorOptions  validator.Options
	signer            ekm.BeaconSigner
	StorageMap        *ibftstorage.ParticipantStores
	validatorsMap     *validators.ValidatorsMap
	validatorStore    registrystorage.ValidatorStore
	operatorDataStore operatordatastore.OperatorDataStore
	operatorStorage   registrystorage.Operators
	networkConfig     networkconfig.NetworkConfig
}

func TestNewController(t *testing.T) {
	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))

	operatorSigner, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	_, logger, _, network, _, recipientStorage, bc := setupCommonTestComponents(t, operatorSigner)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	registryStorage, newStorageErr := storage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
	require.NoError(t, newStorageErr)

	controllerOptions := ControllerOptions{
		NetworkConfig:     networkconfig.TestNetwork,
		Beacon:            bc,
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
	passedEpoch, exitEpoch := phase0.Epoch(1), goclient.FarFutureEpoch
	operators := buildOperators(t)

	operatorDataStore := operatordatastore.New(buildOperatorData(0, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")

	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	operatorStore, done := newOperatorStorageForTest(zap.NewNop())
	defer done()

	sharesWithMetadata := []*types.SSVShare{
		{
			Share: spectypes.Share{
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
			Status:                    v1.ValidatorStateActiveOngoing,
			ActivationEpoch:           passedEpoch,
			ExitEpoch:                 exitEpoch,
			Liquidated:                false,
			BeaconMetadataLastUpdated: time.Now(),
		},
		{
			Share: spectypes.Share{
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey2.GetPublicKey().Serialize()),
			},
			Status:                    v1.ValidatorStateActiveOngoing,
			ActivationEpoch:           passedEpoch,
			ExitEpoch:                 exitEpoch,
			Liquidated:                false,
			BeaconMetadataLastUpdated: time.Now(),
		},
	}
	_ = sharesWithMetadata

	sharesWithoutMetadata := []*types.SSVShare{
		{
			Share: spectypes.Share{
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
			Liquidated: false,
		},
		{
			Share: spectypes.Share{
				Committee:       operators,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey2.GetPublicKey().Serialize()),
			},
			Liquidated: false,
		},
	}
	_ = sharesWithoutMetadata

	testCases := []struct {
		name                       string
		shareStorageListResponse   []*types.SSVShare
		syncHighestDecidedResponse error
		getValidatorDataResponse   error
	}{
		{"no shares of non committee", nil, nil, nil},
		{"set up non committee validators", sharesWithMetadata, nil, nil},
		{"set up non committee validators without metadata", sharesWithoutMetadata, nil, nil},
		{"fail to sync highest decided", sharesWithMetadata, errors.New("failed to sync highest decided"), nil},
		{"fail to update validators metadata", sharesWithMetadata, nil, errors.New("could not update all validators")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operatorPrivateKey, err := keys.GeneratePrivateKey()
			require.NoError(t, err)

			ctrl, logger, sharesStorage, network, _, recipientStorage, bc := setupCommonTestComponents(t, operatorPrivateKey)

			defer ctrl.Finish()
			mockValidatorsMap := validators.New(context.TODO())

			subnets := [commons.SubnetsCount]byte{}
			for _, share := range sharesWithMetadata {
				subnets[commons.CommitteeSubnet(share.CommitteeID())] = 1
			}

			network.EXPECT().ActiveSubnets().Return(subnets[:]).AnyTimes()
			network.EXPECT().FixedSubnets().Return(make(commons.Subnets, commons.SubnetsCount)).AnyTimes()

			if tc.shareStorageListResponse == nil {
				sharesStorage.EXPECT().List(gomock.Any(), gomock.Any()).Return(tc.shareStorageListResponse).Times(1)
			} else {
				sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ basedb.Reader, pubKey []byte) (*types.SSVShare, bool) {
					for _, share := range tc.shareStorageListResponse {
						if hex.EncodeToString(share.Share.ValidatorPubKey[:]) == hex.EncodeToString(pubKey) {
							return share, true
						}
					}
					return nil, false
				}).AnyTimes()
				sharesStorage.EXPECT().List(gomock.Any(), gomock.Any()).Return(tc.shareStorageListResponse).AnyTimes()
				sharesStorage.EXPECT().Range(gomock.Any(), gomock.Any()).DoAndReturn(func(_ basedb.Reader, fn func(*types.SSVShare) bool) {
					for _, share := range tc.shareStorageListResponse {
						if !fn(share) {
							break
						}
					}
				}).AnyTimes()
				recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).Return(recipientData, true, nil).AnyTimes()
			}

			mockValidatorStore := registrystoragemocks.NewMockValidatorStore(ctrl)
			mockValidatorStore.EXPECT().OperatorValidators(gomock.Any()).Return(sharesWithMetadata).AnyTimes()

			validatorStartFunc := func(validator *validator.Validator) (bool, error) {
				return true, nil
			}
			controllerOptions := MockControllerOptions{
				beacon:            bc,
				network:           network,
				operatorDataStore: operatorDataStore,
				operatorStorage:   operatorStore,
				sharesStorage:     sharesStorage,
				recipientsStorage: recipientStorage,
				validatorsMap:     mockValidatorsMap,
				validatorStore:    mockValidatorStore,
				validatorOptions: validator.Options{
					Exporter: true,
				},
			}
			ctr := setupController(logger, controllerOptions)
			ctr.validatorStartFunc = validatorStartFunc
			ctr.StartValidators(context.TODO())
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

	ctr.messageWorker.UseHandler(func(msg network.DecodedSSVMessage) error {
		wg.Done()
		return nil
	})

	wg.Add(3)

	identifier := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, []byte("pk"), spectypes.RoleCommittee)

	ctr.messageRouter.Route(context.TODO(), &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   identifier,
			Data:    generateDecidedMessage(t, identifier),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   identifier,
			Data:    generateChangeRoundMsg(t, identifier),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{ // checks that not process unnecessary message
			MsgType: message.SSVSyncMsgType,
			MsgID:   identifier,
			Data:    []byte("data"),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{ // checks that not process unnecessary message
			MsgType: 123,
			MsgID:   identifier,
			Data:    []byte("data"),
		},
	})

	ctr.messageRouter.Route(context.TODO(), &queue.SSVMessage{
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

func TestSetupValidators(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)

	// Init global variables
	activationEpoch, exitEpoch := phase0.Epoch(1), goclient.FarFutureEpoch
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

	shareKey, err := createKey()
	require.NoError(t, err)

	shareWithMetaData := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  1,
			Committee:       operators[:1],
			ValidatorPubKey: spectypes.ValidatorPK(validatorKey),
			SharePubKey:     shareKey,
		},
		Status:                    v1.ValidatorStateActiveOngoing,
		ActivationEpoch:           activationEpoch,
		ExitEpoch:                 exitEpoch,
		BeaconMetadataLastUpdated: time.Now(),
	}

	shareWithoutMetaData := &types.SSVShare{
		Share: spectypes.Share{
			Committee:       operators[:1],
			ValidatorPubKey: spectypes.ValidatorPK(validatorKey),
			SharePubKey:     shareKey,
		},
		OwnerAddress: common.BytesToAddress([]byte("62Ce5c69260bd819B4e0AD13f4b873074D479811")),
	}

	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")
	ownerAddressBytes := decodeHex(t, "67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	feeRecipientBytes := decodeHex(t, "45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")
	testValidator := setupTestValidator(ownerAddressBytes, feeRecipientBytes)

	opStorage, done := newOperatorStorageForTest(logger)
	defer done()

	opStorage.SaveOperatorData(nil, buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))

	bcResponse := map[phase0.ValidatorIndex]*v1.Validator{
		0: {
			Status: v1.ValidatorStateActiveOngoing,
			Index:  2,
			Validator: &phase0.Validator{
				ActivationEpoch: activationEpoch,
				ExitEpoch:       exitEpoch,
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
		bcResponse         map[phase0.ValidatorIndex]*v1.Validator
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
			sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ basedb.Reader, pubKey []byte) (*types.SSVShare, bool) {
				return shareWithMetaData, true
			}).AnyTimes()

			testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
				createPubKey(byte('0')): testValidator,
			}
			committeeMap := make(map[spectypes.CommitteeID]*validator.Committee)
			mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, committeeMap))

			bc.EXPECT().GetBeaconNetwork().Return(networkconfig.TestNetwork.Beacon.GetBeaconNetwork()).AnyTimes()

			// Set up the controller with mock data
			controllerOptions := MockControllerOptions{
				beacon:            bc,
				network:           network,
				networkConfig:     networkconfig.TestNetwork,
				sharesStorage:     sharesStorage,
				operatorDataStore: operatorDataStore,
				recipientsStorage: recipientStorage,
				operatorStorage:   opStorage,
				validatorsMap:     mockValidatorsMap,
				validatorOptions: validator.Options{
					NetworkConfig: networkconfig.TestNetwork,
					Storage:       storageMap,
				},
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
		Share: &types.SSVShare{},
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
	activationEpoch, exitEpoch := phase0.Epoch(1), goclient.FarFutureEpoch

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
				Status:          v1.ValidatorStateActiveOngoing,
				ActivationEpoch: activationEpoch,
				ExitEpoch:       exitEpoch,
			},
			{
				Share: spectypes.Share{
					Committee: operators[1:],
				},
				Status: v1.ValidatorStatePendingInitialized, // Some other status
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
				Status: v1.ValidatorStateActiveOngoing,
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
				Status: v1.ValidatorStateActiveOngoing,
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
				Status: v1.ValidatorStateActiveOngoing,
			},
			{
				Share: spectypes.Share{
					Committee: operators,
				},
				Status: v1.ValidatorStatePendingInitialized,
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

func setupController(logger *zap.Logger, opts MockControllerOptions) controller {
	// Default to test network config if not provided.
	if opts.networkConfig.Name == "" {
		opts.networkConfig = networkconfig.TestNetwork
	}

	return controller{
		logger:                  logger,
		beacon:                  opts.beacon,
		network:                 opts.network,
		ibftStorageMap:          opts.StorageMap,
		operatorDataStore:       opts.operatorDataStore,
		sharesStorage:           opts.sharesStorage,
		operatorsStorage:        opts.operatorStorage,
		validatorsMap:           opts.validatorsMap,
		validatorStore:          opts.validatorStore,
		ctx:                     context.Background(),
		validatorOptions:        opts.validatorOptions,
		recipientsStorage:       opts.recipientsStorage,
		networkConfig:           opts.networkConfig,
		messageRouter:           newMessageRouter(logger),
		committeeValidatorSetup: make(chan struct{}),
		indicesChange:           make(chan struct{}, 32),
		messageWorker: worker.NewWorker(logger, &worker.Config{
			Ctx:          context.Background(),
			WorkersCount: 1,
			Buffer:       100,
		}),
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

		Share: &types.SSVShare{
			Share: spectypes.Share{
				FeeRecipientAddress: common.BytesToAddress(feeRecipientBytes),
			},
			OwnerAddress: common.BytesToAddress(ownerAddressBytes),
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
		PublicKey:    []byte(base64.StdEncoding.EncodeToString([]byte("samplePublicKey"))),
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

func setupCommonTestComponents(t *testing.T, operatorPrivKey keys.OperatorPrivateKey) (*gomock.Controller, *zap.Logger, *mocks.MockSharesStorage, *mocks.MockP2PNetwork, ekm.KeyManager, *mocks.MockRecipients, *beacon.MockBeaconNode) {
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	bc := beacon.NewMockBeaconNode(ctrl)
	network := mocks.NewMockP2PNetwork(ctrl)
	sharesStorage := mocks.NewMockSharesStorage(ctrl)
	recipientStorage := mocks.NewMockRecipients(ctrl)

	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	km, err := ekm.NewLocalKeyManager(logger, db, networkconfig.TestNetwork, operatorPrivKey)
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
