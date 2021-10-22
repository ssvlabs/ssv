package goclient

import (
	"encoding/hex"
	"fmt"
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

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

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

func TestOpeningAccounts(t *testing.T) {
	wallet, storage := testWallet(t)
	defer storage.(*signerStorage).db.Close()
	seed := _byteArray("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1fff")

	for i := 0; i < 10; i++ {
		testName := fmt.Sprintf("adding and fetching account: %d", i)
		t.Run(testName, func(t *testing.T) {
			// create
			a, err := wallet.CreateValidatorAccount(seed, nil)
			require.NoError(t, err)

			// open
			a1, err := wallet.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
			require.NoError(t, err)

			a2, err := wallet.AccountByID(a.ID())
			require.NoError(t, err)

			// verify
			for _, fetchedAccount := range []core.ValidatorAccount{a1, a2} {
				require.Equal(t, a.ID().String(), fetchedAccount.ID().String())
				require.Equal(t, a.Name(), fetchedAccount.Name())
				require.Equal(t, a.ValidatorPublicKey(), fetchedAccount.ValidatorPublicKey())
				require.Equal(t, a.WithdrawalPublicKey(), fetchedAccount.WithdrawalPublicKey())
			}
		})
	}
}

func TestDeleteAccount(t *testing.T) {
	_, storage := testWallet(t)
	defer storage.(*signerStorage).db.Close()

	accts, err := storage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accts, 1)

	require.NoError(t, storage.DeleteAccount(accts[0].ID()))
	acc, err := storage.OpenAccount(accts[0].ID())
	require.EqualError(t, err, "failed to open wallet")
	require.Nil(t, acc)
}

func TestNonExistingWallet(t *testing.T) {
	storage := getWalletStorage(t)
	w, err := storage.OpenWallet()
	require.NotNil(t, err)
	require.EqualError(t, err, "could not find wallet")
	require.Nil(t, w)
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
