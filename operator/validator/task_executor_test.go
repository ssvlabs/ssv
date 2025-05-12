package validator

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/utils/threshold"
)

func TestController_LiquidateCluster(t *testing.T) {
	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	firstValidator := &validator.Validator{
		DutyRunners: runner.ValidatorDutyRunners{},

		Share: &types.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
		},
	}

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	ctrl, logger, sharesStorage, network, _, recipientStorage, bc := setupCommonTestComponents(t, operatorPrivateKey)
	defer ctrl.Finish()
	testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
		spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()): firstValidator,
	}
	mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))

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
		validatorOptions:  validator.Options{},
	}
	ctr := setupController(logger, controllerOptions)
	ctr.validatorStartFunc = validatorStartFunc

	require.Equal(t, mockValidatorsMap.SizeValidators(), 1)
	_, ok := mockValidatorsMap.GetValidator(spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()))
	require.True(t, ok, "validator not found")

	err = ctr.LiquidateCluster(common.HexToAddress("123"), []uint64{1, 2, 3, 4}, []*types.SSVShare{{Share: spectypes.Share{
		ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
	}}})
	require.NoError(t, err)

	require.Equal(t, mockValidatorsMap.SizeValidators(), 0)
	_, ok = mockValidatorsMap.GetValidator(spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()))
	require.False(t, ok, "validator still exists")
}

type signable struct {
}

func (signable) GetRoot() ([32]byte, error) {
	return sha256.Sum256([]byte("justdata")), nil
}

func TestController_StopValidator(t *testing.T) {
	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	firstValidator := &validator.Validator{
		DutyRunners: runner.ValidatorDutyRunners{},

		Share: &types.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
			},
		},
	}

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	ctrl, logger, sharesStorage, network, signer, recipientStorage, bc := setupCommonTestComponents(t, operatorPrivateKey)

	defer ctrl.Finish()

	testValidatorsMap := map[spectypes.ValidatorPK]*validator.Validator{
		spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()): firstValidator,
	}
	mockValidatorsMap := validators.New(context.TODO(), validators.WithInitialState(testValidatorsMap, nil))

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
		validatorOptions:  validator.Options{},
		signer:            signer,
	}
	ctr := setupController(logger, controllerOptions)
	ctr.validatorStartFunc = validatorStartFunc

	encryptedSharePrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(secretKey.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, signer.AddShare(context.Background(), encryptedSharePrivKey, phase0.BLSPubKey(secretKey.GetPublicKey().Serialize())))

	testingBC := testingutils.NewTestingBeaconNode()
	d, err := testingBC.DomainData(1, spectypes.DomainSyncCommittee)
	require.NoError(t, err)

	root, err := signable{}.GetRoot()
	require.NoError(t, err)

	slot := phase0.Slot(1)

	_, _, err = signer.SignBeaconObject(
		context.Background(),
		spectypes.SSZBytes(root[:]),
		d,
		phase0.BLSPubKey(secretKey.GetPublicKey().Serialize()),
		slot,
		spectypes.DomainSyncCommittee,
	)
	require.NoError(t, err)

	require.Equal(t, mockValidatorsMap.SizeValidators(), 1)
	_, ok := mockValidatorsMap.GetValidator(spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()))
	require.True(t, ok, "validator not found")

	err = ctr.StopValidator(spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()))
	require.NoError(t, err)

	require.Equal(t, mockValidatorsMap.SizeValidators(), 0)
	_, ok = mockValidatorsMap.GetValidator(spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()))
	require.False(t, ok, "validator still exists")
}

func TestController_ReactivateCluster(t *testing.T) {
	storageMap := ibftstorage.NewStores()
	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	shares1, err := threshold.Create(secretKey.Serialize(), 3, 4)
	require.NoError(t, err)
	shares2, err := threshold.Create(secretKey2.Serialize(), 3, 4)
	require.NoError(t, err)

	operatorPrivKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	ctrl, logger, sharesStorage, network, signer, recipientStorage, bc := setupCommonTestComponents(t, operatorPrivKey)
	defer ctrl.Finish()
	mockValidatorsMap := validators.New(context.TODO())
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
		networkConfig:     networkconfig.TestNetwork,
		validatorOptions: validator.Options{
			Storage:       storageMap,
			NetworkConfig: networkconfig.TestNetwork,
		},
		signer: signer,
	}
	ctr := setupController(logger, controllerOptions)
	ctr.validatorStartFunc = validatorStartFunc
	ctr.indicesChange = make(chan struct{})

	encryptedPrivKey, err := operatorPrivKey.Public().Encrypt([]byte(secretKey.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, signer.AddShare(context.Background(), encryptedPrivKey, phase0.BLSPubKey(secretKey.GetPublicKey().Serialize())))

	testingBC := testingutils.NewTestingBeaconNode()
	d, err := testingBC.DomainData(1, spectypes.DomainSyncCommittee)
	require.NoError(t, err)

	root, err := signable{}.GetRoot()
	require.NoError(t, err)

	slot := phase0.Slot(1)

	_, _, err = signer.SignBeaconObject(
		context.Background(),
		spectypes.SSZBytes(root[:]),
		d,
		phase0.BLSPubKey(secretKey.GetPublicKey().Serialize()),
		slot,
		spectypes.DomainSyncCommittee,
	)
	require.NoError(t, err)

	require.Equal(t, mockValidatorsMap.SizeValidators(), 0)
	toReactivate := []*types.SSVShare{
		{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
				SharePubKey:     shares1[1].GetPublicKey().Serialize(),
			},
			Status:          v1.ValidatorStateActiveOngoing, // ValidatorStateUnknown
			ActivationEpoch: 1,
		},
		{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK(secretKey2.GetPublicKey().Serialize()),
				SharePubKey:     shares2[1].GetPublicKey().Serialize(),
			},
			Status:          v1.ValidatorStateActiveOngoing, // ValidatorStateUnknown
			ActivationEpoch: 1,
		},
	}
	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")
	recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).AnyTimes().Return(recipientData, true, nil)

	bc.EXPECT().GetBeaconNetwork().AnyTimes().Return(testingBC.GetBeaconNetwork())

	indiciesUpdate := make(chan struct{})
	go func() {
		<-ctr.indicesChange
		indiciesUpdate <- struct{}{}
	}()
	err = ctr.ReactivateCluster(common.HexToAddress("0x1231231"), []uint64{1, 2, 3, 4}, toReactivate)

	require.NoError(t, err)
	require.Equal(t, mockValidatorsMap.SizeValidators(), 2)
	_, ok := mockValidatorsMap.GetValidator(spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()))
	require.True(t, ok, "validator not found")
	_, ok = mockValidatorsMap.GetValidator(spectypes.ValidatorPK(secretKey2.GetPublicKey().Serialize()))
	require.True(t, ok, "validator not found")

	select {
	case <-indiciesUpdate:
		break
	case <-time.After(1 * time.Second):
		require.Fail(t, "didn't get indices update")
	}

}
