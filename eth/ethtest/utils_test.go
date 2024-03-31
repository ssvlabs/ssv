package ethtest

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventhandler"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/simulator"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/keys"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/blskeygen"
	"github.com/bloxapp/ssv/utils/threshold"
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

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, true, "")
	if err != nil {
		return nil, nil, nil, nil, err
	}

	ctrl := gomock.NewController(t)
	bc := beacon.NewMockBeaconNode(ctrl)

	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	if err != nil {
		return nil, nil, nil, nil, err
	}

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
			bc,
			storageMap,
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
		KeyManager:        keyManager,
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
		bc,
		storageMap,
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

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	encodedPubKey, err := operator.privateKey.Public().Base64()
	if err != nil {
		logger.Fatal("failed to encode operator public key", zap.Error(err))
	}

	privKeyHash, err := operator.privateKey.StorageHash()
	if err != nil {
		logger.Fatal("failed to encode operator private key", zap.Error(err))
	}

	if err := nodeStorage.SavePrivateKeyHash(privKeyHash); err != nil {
		logger.Fatal("couldn't setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKeyHash()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, encodedPubKey)
	if err != nil {
		logger.Fatal("couldn't get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey:    encodedPubKey,
			ID:           operator.id,
			OwnerAddress: *ownerAddress,
		}
	}

	return nodeStorage, operatorData
}

func simTestBackend(testAddresses []*ethcommon.Address) *simulator.SimulatedBackend {
	genesis := core.GenesisAlloc{}

	for _, testAddr := range testAddresses {
		genesis[*testAddr] = core.GenesisAccount{Balance: big.NewInt(10000000000000000)}
	}

	return simulator.NewSimulatedBackend(
		genesis, 50_000_000,
	)
}
