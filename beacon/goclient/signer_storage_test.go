package goclient

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/encryptor/keystorev4"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func getWalletStorage(t *testing.T) core.Storage {
	options := basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	}
	db, err := storage.GetStorageFactory(options)
	require.NoError(t, err)

	return newSignerStorage(db, core.PraterNetwork)
}

func testWallet(t *testing.T) (core.Wallet, core.Storage) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()
	index := 1

	storage := getWalletStorage(t)

	wallet := hd.NewWallet(&core.WalletContext{Storage: storage})
	require.NoError(t, storage.SaveWallet(wallet))

	_, err := wallet.CreateValidatorAccountFromPrivateKey(sk.Serialize(), &index)
	require.NoError(t, err)

	return wallet, storage
}

func TestWalletStorage(t *testing.T) {
	tests := []struct {
		name       string
		walletName string
		encryptor  encryptor.Encryptor
		password   []byte
		error
	}{
		{
			name:       "serialization and fetching",
			walletName: "test1",
		},
		{
			name:       "serialization and fetching with encryptor",
			walletName: "test2",
			encryptor:  keystorev4.New(),
			password:   []byte("password"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wallet, storage := testWallet(t)
			defer storage.(*signerStorage).db.Close()

			// set encryptor
			if test.encryptor != nil {
				storage.SetEncryptor(test.encryptor, test.password)
			} else {
				storage.SetEncryptor(nil, nil)
			}

			err := storage.SaveWallet(wallet)
			if err != nil {
				if test.error != nil {
					require.Equal(t, test.error.Error(), err.Error())
				} else {
					t.Error(err)
				}
				return
			}

			// fetch wallet by id
			fetched, err := storage.OpenWallet()
			if err != nil {
				if test.error != nil {
					require.Equal(t, test.error.Error(), err.Error())
				} else {
					t.Error(err)
				}
				return
			}

			require.NotNil(t, fetched)
			require.NoError(t, test.error)

			// assert
			require.Equal(t, wallet.ID(), fetched.ID())
			require.Equal(t, wallet.Type(), fetched.Type())
		})
	}
}
