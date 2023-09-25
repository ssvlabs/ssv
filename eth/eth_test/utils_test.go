package eth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventhandler"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/simulator"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/blskeygen"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"

	"testing"
)

type testValidatorData struct {
	masterKey        *bls.SecretKey
	masterPubKey     *bls.PublicKey
	masterPublicKeys bls.PublicKeys
	operatorsShares  []*testShare
}

type testOperator struct {
	id   uint64
	pub  []byte // rsa pub
	priv []byte // rsa sk
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

	msk, pubk := blskeygen.GenBLSKeyPair()
	secVec := msk.GetMasterSecretKey(int(sharesCount))
	pubks := bls.GetMasterPublicKey(secVec)
	splitKeys, err := threshold.Create(msk.Serialize(), sharesCount-1, sharesCount)
	if err != nil {
		return nil, err
	}

	num := uint64(len(ops))
	validatorData.operatorsShares = make([]*testShare, num)

	// derive a `hareCount` number of shares
	for i := uint64(1); i <= num; i++ {
		validatorData.operatorsShares[i-1] = &testShare{
			opId: i,
			sec:  splitKeys[i],
			pub:  splitKeys[i].GetPublicKey(),
		}
	}

	validatorData.masterKey = msk
	validatorData.masterPubKey = pubk
	validatorData.masterPublicKeys = pubks

	return validatorData, nil
}

func createOperators(num uint64, idOffset uint64) ([]*testOperator, error) {
	testops := make([]*testOperator, num)

	for i := uint64(1); i <= num; i++ {
		pb, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			return nil, err
		}
		testops[i-1] = &testOperator{
			id:   idOffset + i,
			pub:  pb,
			priv: sk,
		}
	}

	return testops, nil
}

func generateSharesData(validatorData *testValidatorData, operators []*testOperator, owner ethcommon.Address, nonce int) ([]byte, error) {
	var pubkeys []byte
	var encryptedShares []byte

	for i, op := range operators {
		rsakey, err := rsaencryption.ConvertPemToPublicKey(op.pub)
		if err != nil {
			return nil, fmt.Errorf("cant convert publickey: %w", err)
		}

		rawshare := validatorData.operatorsShares[i].sec.SerializeToHexStr()
		ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, rsakey, []byte(rawshare))
		if err != nil {
			return nil, errors.New("cant encrypt share")
		}

		rsapriv, err := rsaencryption.ConvertPemToPrivateKey(string(op.priv))
		if err != nil {
			return nil, err
		}

		// check that we encrypt right
		shareSecret := &bls.SecretKey{}
		decryptedSharePrivateKey, err := rsaencryption.DecodeKey(rsapriv, ciphertext)
		if err != nil {
			return nil, err
		}
		if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
			return nil, err
		}

		pubkeys = append(pubkeys, validatorData.operatorsShares[i].pub.Serialize()...)
		encryptedShares = append(encryptedShares, ciphertext...)

	}

	tosign := fmt.Sprintf("%s:%d", owner.String(), nonce)
	msghash := crypto.Keccak256([]byte(tosign))
	signed := validatorData.masterKey.Sign(string(msghash))
	sig := signed.Serialize()

	if !signed.VerifyByte(validatorData.masterPubKey, msghash) {
		return nil, errors.New("couldn't sign correctly")
	}

	sharesData := append(pubkeys, encryptedShares...)
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
) (*eventhandler.EventHandler, *mocks.MockController, operatorstorage.Storage, error) {
	db, err := kv.NewInMemory(logger, basedb.Options{
		Ctx: ctx,
	})
	require.NoError(t, err)

	storageMap := ibftstorage.NewStores()
	nodeStorage, operatorData := setupOperatorStorage(logger, db, operator, ownerAddress)
	testNetworkConfig := networkconfig.TestNetwork

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, testNetworkConfig, true, "")
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	bc := beacon.NewMockBeaconNode(ctrl)
	contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	require.NoError(t, err)

	if useMockCtrl {
		validatorCtrl := mocks.NewMockController(ctrl)

		parser := eventparser.New(contractFilterer)

		eh, err := eventhandler.New(
			nodeStorage,
			parser,
			validatorCtrl,
			testNetworkConfig.Domain,
			validatorCtrl,
			nodeStorage.GetPrivateKey,
			keyManager,
			bc,
			storageMap,
			eventhandler.WithFullNode(),
			eventhandler.WithLogger(logger),
		)
		require.NoError(t, err)

		validatorCtrl.EXPECT().GetOperatorData().Return(operatorData).AnyTimes()

		return eh, validatorCtrl, nodeStorage, nil
	}

	validatorCtrl := validator.NewController(logger, validator.ControllerOptions{
		Context:         ctx,
		DB:              db,
		RegistryStorage: nodeStorage,
		KeyManager:      keyManager,
		StorageMap:      storageMap,
		OperatorData:    operatorData,
	})

	parser := eventparser.New(contractFilterer)

	eh, err := eventhandler.New(
		nodeStorage,
		parser,
		validatorCtrl,
		testNetworkConfig.Domain,
		validatorCtrl,
		nodeStorage.GetPrivateKey,
		keyManager,
		bc,
		storageMap,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger),
	)
	require.NoError(t, err)

	return eh, nil, nodeStorage, nil
}

func setupOperatorStorage(
	logger *zap.Logger,
	db basedb.Database,
	operator *testOperator,
	ownerAddress *ethcommon.Address,
) (operatorstorage.Storage, *registrystorage.OperatorData) {
	if operator == nil {
		logger.Fatal("empty test operator was passed", zap.Error(fmt.Errorf("empty test operator was passed")))
	}

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	operatorPubKey, err := nodeStorage.SetupPrivateKey(base64.StdEncoding.EncodeToString(operator.priv))
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(nil, operatorPubKey)

	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey:    operatorPubKey,
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
		genesis, 10000000,
	)
}
