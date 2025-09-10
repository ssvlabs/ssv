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
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/keys"

	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/threshold"
)

func TestController_LiquidateCluster(t *testing.T) {
	operatorDataStore := operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"))
	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(secretKeyStrings[0]))
	require.NoError(t, secretKey2.SetHexString(secretKeyStrings[1]))

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
		beacon:              bc,
		network:             network,
		operatorDataStore:   operatorDataStore,
		sharesStorage:       sharesStorage,
		recipientsStorage:   recipientStorage,
		validatorsMap:       mockValidatorsMap,
		validatorCommonOpts: &validator.CommonOptions{},
	}
	ctr := setupController(t, logger, controllerOptions)
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
	require.NoError(t, secretKey.SetHexString(secretKeyStrings[0]))

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
		beacon:              bc,
		network:             network,
		operatorDataStore:   operatorDataStore,
		sharesStorage:       sharesStorage,
		recipientsStorage:   recipientStorage,
		validatorsMap:       mockValidatorsMap,
		validatorCommonOpts: &validator.CommonOptions{},
		signer:              signer,
	}
	ctr := setupController(t, logger, controllerOptions)
	ctr.validatorStartFunc = validatorStartFunc

	encryptedSharePrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(secretKey.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, signer.AddShare(t.Context(), nil, encryptedSharePrivKey, phase0.BLSPubKey(secretKey.GetPublicKey().Serialize())))

	testingBC := testingutils.NewTestingBeaconNode()
	d, err := testingBC.DomainData(1, spectypes.DomainSyncCommittee)
	require.NoError(t, err)

	root, err := signable{}.GetRoot()
	require.NoError(t, err)

	slot := phase0.Slot(1)

	_, _, err = signer.SignBeaconObject(
		t.Context(),
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
	testCtx := t.Context()
	numberOfValidators := 4

	secretKeys := make([]*bls.SecretKey, 0, numberOfValidators)
	toReactivate := make([]*types.SSVShare, 0, numberOfValidators)

	for i := range numberOfValidators {
		secretKey := &bls.SecretKey{}
		require.NoError(t, secretKey.SetHexString(secretKeyStrings[i]))
		secretKeys = append(secretKeys, secretKey)

		share, err := threshold.Create(secretKey.Serialize(), 3, 4)
		require.NoError(t, err)

		byteShare := share[1].GetPublicKey().Serialize()

		shareToReactivate := types.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  phase0.ValidatorIndex(i + 1),
				ValidatorPubKey: spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize()),
				SharePubKey:     byteShare,
			},
			Status:          v1.ValidatorStateActiveOngoing,
			ActivationEpoch: 1,
		}

		toReactivate = append(toReactivate, &shareToReactivate)
	}

	operatorPrivKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	ctrl, logger, sharesStorage, network, signer, recipientStorage, bc := setupCommonTestComponents(t, operatorPrivKey)
	defer ctrl.Finish()

	mockValidatorsMap := validators.New(testCtx)

	operatorStore, done := newOperatorStorageForTest(log.TestLogger(t))
	defer done()

	validatorStartFunc := func(validator *validator.Validator) (bool, error) {
		return true, nil
	}

	controllerOptions := MockControllerOptions{
		beacon:            bc,
		network:           network,
		operatorDataStore: operatordatastore.New(buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")),
		sharesStorage:     sharesStorage,
		recipientsStorage: recipientStorage,
		validatorsMap:     mockValidatorsMap,
		networkConfig:     networkconfig.TestNetwork,
		validatorCommonOpts: &validator.CommonOptions{
			Storage:       ibftstorage.NewStores(),
			NetworkConfig: networkconfig.TestNetwork,
		},
		signer:          signer,
		operatorStorage: operatorStore,
	}
	ctr := setupController(t, logger, controllerOptions)
	ctr.validatorStartFunc = validatorStartFunc
	ctr.indicesChangeCh = make(chan struct{})

	encryptedPrivKey, err := operatorPrivKey.Public().Encrypt([]byte(secretKeys[0].SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, signer.AddShare(testCtx, nil, encryptedPrivKey, phase0.BLSPubKey(secretKeys[0].GetPublicKey().Serialize())))

	testingBC := testingutils.NewTestingBeaconNode()
	domain, err := testingBC.DomainData(1, spectypes.DomainSyncCommittee)
	require.NoError(t, err)

	root, err := signable{}.GetRoot()
	require.NoError(t, err)

	slot := phase0.Slot(1)

	_, _, err = signer.SignBeaconObject(
		testCtx,
		spectypes.SSZBytes(root[:]),
		domain,
		phase0.BLSPubKey(secretKeys[0].GetPublicKey().Serialize()),
		slot,
		spectypes.DomainSyncCommittee,
	)
	require.NoError(t, err)

	require.Equal(t, mockValidatorsMap.SizeValidators(), 0)

	operatorIDs := []spectypes.OperatorID{1, 2, 3, 4}
	committee := make([]*spectypes.ShareMember, 0, len(operatorIDs))

	for i, share := range toReactivate {
		committee = append(committee, &spectypes.ShareMember{
			Signer:      operatorIDs[i],
			SharePubKey: share.SharePubKey,
		})

		_, err := operatorStore.SaveOperatorData(nil, &storage.OperatorData{ID: operatorIDs[i]})
		require.NoError(t, err)
	}

	for _, share := range toReactivate {
		share.Committee = committee
	}

	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")
	recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).AnyTimes().Return(recipientData, true, nil)

	indiciesUpdate := make(chan struct{})
	go func() {
		<-ctr.indicesChangeCh
		indiciesUpdate <- struct{}{}
	}()

	err = ctr.ReactivateCluster(common.HexToAddress("0x1231231"), operatorIDs, toReactivate)

	require.NoError(t, err)
	require.Equal(t, 4, mockValidatorsMap.SizeValidators())

	for _, key := range secretKeys {
		_, ok := mockValidatorsMap.GetValidator(spectypes.ValidatorPK(key.GetPublicKey().Serialize()))
		require.True(t, ok, "validator not found")
	}

	select {
	case <-indiciesUpdate:
		break
	case <-time.After(1 * time.Second):
		require.Fail(t, "didn't get indices update")
	}
}
