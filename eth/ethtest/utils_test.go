package ethtest

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/eventhandler"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/simulator"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validator/mocks"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/blskeygen"
	"github.com/ssvlabs/ssv/utils/threshold"
)

type testValidatorData struct {
	masterKey        *bls.SecretKey
	masterPubKey     *bls.PublicKey
	masterPublicKeys bls.PublicKeys
	operatorsShares  []*testShare
}

type testOperator struct {
	id         uint64
	privateKey keys.OperatorPrivateKey
}

type testShare struct {
	opId uint64
	sec  *bls.SecretKey
	pub  *bls.PublicKey
}

func createNewValidator(ops []*testOperator) (*testValidatorData, error) {
	validatorData := &testValidatorData{}
	sharesCount := uint64(len(ops))
	threshold.Init()

	msk, mpk := blskeygen.GenBLSKeyPair()
	secVec := msk.GetMasterSecretKey(int(sharesCount))
	pubKeys := bls.GetMasterPublicKey(secVec)
	splitKeys, err := threshold.Create(msk.Serialize(), sharesCount-1, sharesCount)
	if err != nil {
		return nil, err
	}

	validatorData.operatorsShares = make([]*testShare, sharesCount)

	// derive a `sharesCount` number of shares
	for i := uint64(1); i <= sharesCount; i++ {
		validatorData.operatorsShares[i-1] = &testShare{
			opId: i,
			sec:  splitKeys[i],
			pub:  splitKeys[i].GetPublicKey(),
		}
	}

	validatorData.masterKey = msk
	validatorData.masterPubKey = mpk
	validatorData.masterPublicKeys = pubKeys

	return validatorData, nil
}

func createOperators(num uint64, idOffset uint64) ([]*testOperator, error) {
	testOps := make([]*testOperator, num)

	for i := uint64(1); i <= num; i++ {
		privateKey, err := keys.GeneratePrivateKey()
		if err != nil {
			return nil, err
		}

		testOps[i-1] = &testOperator{
			id:         idOffset + i,
			privateKey: privateKey,
		}
	}

	return testOps, nil
}

func generateSharesData(validatorData *testValidatorData, operators []*testOperator, owner ethcommon.Address, nonce int) ([]byte, error) {
	var pubKeys []byte
	var encryptedShares []byte

	for i, op := range operators {
		rawShare := validatorData.operatorsShares[i].sec.SerializeToHexStr()

		cipherText, err := op.privateKey.Public().Encrypt([]byte(rawShare))
		if err != nil {
			return nil, fmt.Errorf("can't encrypt share: %w", err)
		}

		// check that we encrypt right
		shareSecret := &bls.SecretKey{}
		decryptedSharePrivateKey, err := op.privateKey.Decrypt(cipherText)
		if err != nil {
			return nil, err
		}
		if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
			return nil, err
		}

		pubKeys = append(pubKeys, validatorData.operatorsShares[i].pub.Serialize()...)
		encryptedShares = append(encryptedShares, cipherText...)

	}

	toSign := fmt.Sprintf("%s:%d", owner.String(), nonce)
	msgHash := crypto.Keccak256([]byte(toSign))
	signed := validatorData.masterKey.Sign(string(msgHash))
	sig := signed.Serialize()

	if !signed.VerifyByte(validatorData.masterPubKey, msgHash) {
		return nil, errors.New("can't sign correctly")
	}

	sharesData := append(pubKeys, encryptedShares...)
	sharesDataSigned := append(sig, sharesData...)

	return sharesDataSigned, nil
}

func setupEventHandler(
	t *testing.T,
	ctx context.Context,
	logger *zap.Logger,
	operator *testOperator,
	ownerAddress *ethcommon.Address,
	useMockCtrl bool,
) (*eventhandler.EventHandler, *mocks.MockController, *gomock.Controller, operatorstorage.Storage, error) {
	db, err := kv.NewInMemory(logger, basedb.Options{
		Ctx: ctx,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db, operator, ownerAddress)
	operatorDataStore := operatordatastore.New(operatorData)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewLocalKeyManager(logger, db, testNetworkConfig, operator.privateKey)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	ctrl := gomock.NewController(t)

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	dgHandler := doppelganger.NoOpHandler{}

	if useMockCtrl {
		validatorCtrl := mocks.NewMockController(ctrl)

		parser := eventparser.New(contractFilterer)

		eh, err := eventhandler.New(
			nodeStorage,
			parser,
			validatorCtrl,
			testNetworkConfig,
			operatorDataStore,
			operator.privateKey,
			keyManager,
			dgHandler,
			eventhandler.WithFullNode(),
			eventhandler.WithLogger(logger),
		)

		if err != nil {
			return nil, nil, nil, nil, err
		}

		return eh, validatorCtrl, ctrl, nodeStorage, nil
	}

	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{
		Context:           ctx,
		DB:                db,
		RegistryStorage:   nodeStorage,
		BeaconSigner:      keyManager,
		StorageMap:        storageMap,
		OperatorDataStore: operatorDataStore,
	})

	parser := eventparser.New(contractFilterer)

	eh, err := eventhandler.New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig,
		operatorDataStore,
		operator.privateKey,
		keyManager,
		dgHandler,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger),
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return eh, nil, ctrl, nodeStorage, nil
}

func setupOperatorStorage(
	logger *zap.Logger,
	db basedb.Database,
	operator *testOperator,
	ownerAddress *ethcommon.Address,
) (operatorstorage.Storage, *registrystorage.OperatorData) {
	if operator == nil {
		logger.Fatal("empty test operator was passed")
	}

	nodeStorage, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	encodedPubKey, err := operator.privateKey.Public().Base64()
	if err != nil {
		logger.Fatal("failed to encode operator public key", zap.Error(err))
	}

	if err := nodeStorage.SavePrivateKeyHash(operator.privateKey.StorageHash()); err != nil {
		logger.Fatal("couldn't setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKeyHash()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, []byte(encodedPubKey))
	if err != nil {
		logger.Fatal("couldn't get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey:    []byte(encodedPubKey),
			ID:           operator.id,
			OwnerAddress: *ownerAddress,
		}
	}

	return nodeStorage, operatorData
}

func simTestBackend(testAddresses []*ethcommon.Address) *simulator.Backend {
	genesis := types.GenesisAlloc{}

	for _, testAddr := range testAddresses {
		genesis[*testAddr] = types.Account{Balance: big.NewInt(10000000000000000)}
	}

	return simulator.NewBackend(genesis,
		simulated.WithBlockGasLimit(50_000_000),
	)
}
