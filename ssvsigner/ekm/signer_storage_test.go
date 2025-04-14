package ekm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/google/uuid"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/encryptor"
	"github.com/ssvlabs/eth2-key-manager/encryptor/keystorev4"
	"github.com/ssvlabs/eth2-key-manager/wallets/hd"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/threshold"
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

func getBaseStorage(logger *zap.Logger) (basedb.Database, error) {
	return kv.NewInMemory(logger, basedb.Options{})
}

func newStorageForTest(t *testing.T) (Storage, func()) {
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	if err != nil {
		return nil, func() {}
	}

	s := NewSignerStorage(db, networkconfig.TestNetwork.Beacon.GetNetwork(), logger)
	return s, func() {
		db.Close()
	}
}

func testWallet(t *testing.T) (core.Wallet, Storage, func()) {
	threshold.Init()

	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	index := 1

	signerStorage, done := newStorageForTest(t)

	wallet := hd.NewWallet(&core.WalletContext{Storage: signerStorage})
	require.NoError(t, signerStorage.SaveWallet(wallet))

	_, err := wallet.CreateValidatorAccountFromPrivateKey(sk.Serialize(), &index)
	require.NoError(t, err)

	return wallet, signerStorage, done
}

// TestWalletAndAccountManagement tests wallet and account operations.
func TestWalletAndAccountManagement(t *testing.T) {
	t.Run("OpeningAccounts", func(t *testing.T) {
		wallet, _, done := testWallet(t)
		defer done()

		seed := _byteArray("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1fff")

		for i := range 10 {
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
	})

	t.Run("OpenWallet", func(t *testing.T) {
		t.Run("SuccessfulOpen", func(t *testing.T) {
			wallet, signerStorage, done := testWallet(t)
			defer done()

			// Verify original wallet
			require.NotNil(t, wallet)

			// Open wallet and verify
			fetched, err := signerStorage.OpenWallet()
			require.NoError(t, err)
			require.NotNil(t, fetched)
			require.Equal(t, wallet.ID(), fetched.ID())
			require.Equal(t, wallet.Type(), fetched.Type())
		})

		t.Run("NonExistentWallet", func(t *testing.T) {
			signerStorage, done := newStorageForTest(t)
			defer done()

			w, err := signerStorage.OpenWallet()
			require.Error(t, err)
			require.EqualError(t, err, "could not find wallet")
			require.Nil(t, w)
		})

		t.Run("EmptyWalletValue", func(t *testing.T) {
			signerStorage, done := newStorageForTest(t)
			defer done()

			// Use unexported field to set up test condition - store empty value
			s := signerStorage.(*storage)
			err := s.db.Set(s.objPrefix(walletPrefix), []byte(walletPath), []byte{})
			require.NoError(t, err)

			// Attempt to open wallet
			wallet, err := signerStorage.OpenWallet()
			require.Error(t, err)
			require.Contains(t, err.Error(), "failed to open wallet")
			require.Nil(t, wallet)
		})

		t.Run("InvalidWalletJSON", func(t *testing.T) {
			signerStorage, done := newStorageForTest(t)
			defer done()

			// Use unexported field to set up test condition - store invalid JSON
			s := signerStorage.(*storage)
			err := s.db.Set(s.objPrefix(walletPrefix), []byte(walletPath), []byte("{invalid-json}"))
			require.NoError(t, err)

			// Attempt to open wallet
			wallet, err := signerStorage.OpenWallet()
			require.Error(t, err)
			require.Contains(t, err.Error(), "unmarshal HD Wallet object")
			require.Nil(t, wallet)
		})
	})

	t.Run("OpenAccount", func(t *testing.T) {
		t.Run("ExistingAccount", func(t *testing.T) {
			_, signerStorage, done := testWallet(t)
			defer done()

			accounts, err := signerStorage.ListAccounts()
			require.NoError(t, err)
			require.NotEmpty(t, accounts)

			account, err := signerStorage.OpenAccount(accounts[0].ID())
			require.NoError(t, err)
			require.NotNil(t, account)
			require.Equal(t, accounts[0].ID(), account.ID())
		})

		t.Run("NonExistentAccount", func(t *testing.T) {
			wallet, _, done := testWallet(t)
			defer done()

			account, err := wallet.AccountByID(uuid.New())
			require.EqualError(t, err, "account not found")
			require.Nil(t, account)
		})
	})

	t.Run("AccountSerialization", func(t *testing.T) {
		t.Run("ValidAccountRoundtrip", func(t *testing.T) {
			_, signerStorage, done := testWallet(t)
			defer done()

			// Get an existing account
			accounts, err := signerStorage.ListAccounts()
			require.NoError(t, err)
			require.NotEmpty(t, accounts)

			originalAccount := accounts[0]
			accountID := originalAccount.ID()

			// save the account again (+ encode)
			err = signerStorage.SaveAccount(originalAccount)
			require.NoError(t, err)

			// open the account (+ decode)
			retrievedAccount, err := signerStorage.OpenAccount(accountID)
			require.NoError(t, err)
			require.NotNil(t, retrievedAccount)

			require.Equal(t, originalAccount.ID(), retrievedAccount.ID())
			require.Equal(t, originalAccount.Name(), retrievedAccount.Name())
			require.Equal(t,
				originalAccount.ValidatorPublicKey(),
				retrievedAccount.ValidatorPublicKey())
		})
	})

	t.Run("InvalidAccountJSON", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		s := signerStorage.(*storage)
		result, err := s.decodeAccount([]byte("{invalid-json}"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unmarshal HD account object")
		require.Nil(t, result)
	})

	t.Run("ValidAccountJSON", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		// Get an existing account and marshal it
		accounts, err := signerStorage.ListAccounts()
		require.NoError(t, err)
		require.NotEmpty(t, accounts)

		// This is a bit of a hack for testing, but we're assuming the storage format is JSON
		data, err := json.Marshal(accounts[0])
		require.NoError(t, err)

		// Now decode it
		s := signerStorage.(*storage)
		result, err := s.decodeAccount(data)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, accounts[0].ID(), result.ID())
	})

	t.Run("WalletStorage", func(t *testing.T) {
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
				wallet, signerStorage, done := testWallet(t)
				defer done()

				// set encryptor
				if test.encryptor != nil {
					signerStorage.SetEncryptor(test.encryptor, test.password)
				} else {
					signerStorage.SetEncryptor(nil, nil)
				}

				err := signerStorage.SaveWallet(wallet)
				if err != nil {
					if test.error != nil {
						require.Equal(t, test.Error(), err.Error())
					} else {
						t.Error(err)
					}
					return
				}

				// fetch wallet by id
				fetched, err := signerStorage.OpenWallet()
				if err != nil {
					if test.error != nil {
						require.Equal(t, test.Error(), err.Error())
					} else {
						t.Error(err)
					}
					return
				}

				require.NotNil(t, fetched)
				require.NoError(t, test.error)

				require.Equal(t, wallet.ID(), fetched.ID())
				require.Equal(t, wallet.Type(), fetched.Type())
			})
		}
	})

	t.Run("ListAccounts", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		accounts, err := signerStorage.ListAccounts()
		require.NoError(t, err)
		require.Len(t, accounts, 1)
	})

	t.Run("DeleteAccount", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		accts, err := signerStorage.ListAccounts()
		require.NoError(t, err)
		require.Len(t, accts, 1)

		require.NoError(t, signerStorage.DeleteAccount(accts[0].ID()))
		acc, err := signerStorage.OpenAccount(accts[0].ID())
		require.EqualError(t, err, "account not found")
		require.Nil(t, acc)
	})
}

// TestStorageUtilityFunctions tests utility methods in Storage interface.
func TestStorageUtilityFunctions(t *testing.T) {
	t.Parallel()

	t.Run("Name", func(t *testing.T) {
		t.Parallel()

		_, signerStorage, done := testWallet(t)
		defer done()

		name := signerStorage.Name()
		require.Equal(t, "SSV Storage", name)
	})

	t.Run("DropRegistryData", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		err := signerStorage.DropRegistryData()
		require.NoError(t, err)
	})

	t.Run("SetEncryptionKey", func(t *testing.T) {
		t.Parallel()

		logger := logging.TestLogger(t)

		db, err := getBaseStorage(logger)
		require.NoError(t, err)
		defer db.Close()

		signerStorage := NewSignerStorage(db, networkconfig.TestNetwork.Beacon.GetNetwork(), logger)

		err = signerStorage.SetEncryptionKey("aabbccddee")
		require.NoError(t, err)

		err = signerStorage.SetEncryptionKey("invalid-hex-key")
		require.Error(t, err)
		require.Contains(t, err.Error(), "the key must be a valid hexadecimal string")
	})

	t.Run("DataEncryption", func(t *testing.T) {
		t.Parallel()

		logger := logging.TestLogger(t)
		db, err := getBaseStorage(logger)
		require.NoError(t, err)
		defer db.Close()

		signerStorage := NewSignerStorage(db, networkconfig.TestNetwork.Beacon.GetNetwork(), logger)

		// create a test account
		wallet := hd.NewWallet(&core.WalletContext{Storage: signerStorage})
		require.NoError(t, signerStorage.SaveWallet(wallet))

		// generate a private key
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		index := 1

		// create an account
		account, err := wallet.CreateValidatorAccountFromPrivateKey(sk.Serialize(), &index)
		require.NoError(t, err)
		require.NotNil(t, account)
		accountID := account.ID()

		// test with encryption key
		err = signerStorage.SetEncryptionKey("0123456789abcdef0123456789abcdef")
		require.NoError(t, err)

		// save the account (this will encrypt it)
		err = signerStorage.SaveAccount(account)
		require.NoError(t, err)

		// retrieve the account (this will decrypt it)
		retrievedAccount, err := signerStorage.OpenAccount(accountID)
		require.NoError(t, err)
		require.NotNil(t, retrievedAccount)
		require.Equal(t, accountID, retrievedAccount.ID())

		// now test without encryption key
		err = signerStorage.SetEncryptionKey("")
		require.NoError(t, err)

		// save the account again (this won't encrypt it)
		err = signerStorage.SaveAccount(account)
		require.NoError(t, err)

		// retrieve the account (no decryption needed)
		retrievedAccount, err = signerStorage.OpenAccount(accountID)
		require.NoError(t, err)
		require.NotNil(t, retrievedAccount)
		require.Equal(t, accountID, retrievedAccount.ID())
	})
}

// TestSlashingProtection tests slashing protection data storage and retrieval.
func TestSlashingProtection(t *testing.T) {
	t.Run("AttestationData", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		testCases := []struct {
			name    string
			att     *phase0.AttestationData
			account core.ValidatorAccount
		}{
			{
				name: "standard_attestation",
				att: &phase0.AttestationData{
					Slot:            30,
					Index:           1,
					BeaconBlockRoot: [32]byte{},
					Source: &phase0.Checkpoint{
						Epoch: 1,
						Root:  [32]byte{},
					},
					Target: &phase0.Checkpoint{
						Epoch: 4,
						Root:  [32]byte{},
					},
				},
				account: &mockAccount{
					id:            uuid.New(),
					validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
				},
			},
			{
				name: "differing_target_epoch",
				att: &phase0.AttestationData{
					Slot:            30,
					Index:           1,
					BeaconBlockRoot: [32]byte{},
					Source: &phase0.Checkpoint{
						Epoch: 1,
						Root:  [32]byte{},
					},
					Target: &phase0.Checkpoint{
						Epoch: 3,
						Root:  [32]byte{},
					},
				},
				account: &mockAccount{
					id:            uuid.New(),
					validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := signerStorage.SaveHighestAttestation(tc.account.ValidatorPublicKey(), tc.att)
				require.NoError(t, err)

				att, found, err := signerStorage.RetrieveHighestAttestation(tc.account.ValidatorPublicKey())
				require.NoError(t, err)
				require.True(t, found)
				require.NotNil(t, att)

				aRoot, err := att.HashTreeRoot()
				require.NoError(t, err)
				bRoot, err := tc.att.HashTreeRoot()
				require.NoError(t, err)
				require.Equal(t, aRoot, bRoot)
			})
		}

		t.Run("error_nil_pubkey", func(t *testing.T) {
			att := &phase0.AttestationData{
				Slot:  30,
				Index: 1,
			}
			err := signerStorage.SaveHighestAttestation(nil, att)
			require.Error(t, err)
			require.Contains(t, err.Error(), "pubKey must not be nil")

			_, found, err := signerStorage.RetrieveHighestAttestation(nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "public key could not be nil")
			require.False(t, found)
		})

		t.Run("error_nil_attestation", func(t *testing.T) {
			pubKey := []byte("test_pubkey")
			err := signerStorage.SaveHighestAttestation(pubKey, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "attestation data could not be nil")
		})

		t.Run("InvalidAttestationSSZ", func(t *testing.T) {
			_, signerStorage, done := testWallet(t)
			defer done()

			s := signerStorage.(*storage)
			pubKey := []byte("test_pubkey")

			err := s.db.Set(s.objPrefix(highestAttPrefix), pubKey, []byte("invalid-ssz-data"))
			require.NoError(t, err)

			att, found, err := signerStorage.RetrieveHighestAttestation(pubKey)
			require.Error(t, err)
			require.True(t, found)
			require.Contains(t, err.Error(), "could not unmarshal attestation data")
			require.Nil(t, att)
		})

		t.Run("EmptyAttestationValue", func(t *testing.T) {
			_, signerStorage, done := testWallet(t)
			defer done()

			s := signerStorage.(*storage)
			pubKey := []byte("test_pubkey")

			err := s.db.Set(s.objPrefix(highestAttPrefix), pubKey, []byte{})
			require.NoError(t, err)

			att, found, err := signerStorage.RetrieveHighestAttestation(pubKey)
			require.Error(t, err)
			require.True(t, found)
			require.Contains(t, err.Error(), "highest attestation value is empty")
			require.Nil(t, att)
		})

		t.Run("remove_attestation", func(t *testing.T) {
			account := &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			}

			att := &phase0.AttestationData{
				Slot:            30,
				Index:           1,
				BeaconBlockRoot: [32]byte{},
				Source: &phase0.Checkpoint{
					Epoch: 1,
					Root:  [32]byte{},
				},
				Target: &phase0.Checkpoint{
					Epoch: 4,
					Root:  [32]byte{},
				},
			}

			err := signerStorage.SaveHighestAttestation(account.ValidatorPublicKey(), att)
			require.NoError(t, err)

			retrieved, found, err := signerStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, retrieved)

			err = signerStorage.RemoveHighestAttestation(account.ValidatorPublicKey())
			require.NoError(t, err)

			retrieved, found, err = signerStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
			require.NoError(t, err)
			require.False(t, found)
			require.Nil(t, retrieved)
		})
	})

	t.Run("ProposalData", func(t *testing.T) {
		_, signerStorage, done := testWallet(t)
		defer done()

		testCases := []struct {
			name     string
			proposal phase0.Slot
			account  core.ValidatorAccount
		}{
			{
				name:     "simple_proposal",
				proposal: phase0.Slot(1),
				account: &mockAccount{
					id:            uuid.New(),
					validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
				},
			},
			{
				name:     "slot_100",
				proposal: phase0.Slot(100),
				account: &mockAccount{
					id:            uuid.New(),
					validationKey: _bigInt("6467048590701165350380985526996487573957450279098876378395441669247373404219"),
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// save proposal
				err := signerStorage.SaveHighestProposal(tc.account.ValidatorPublicKey(), tc.proposal)
				require.NoError(t, err)

				// retrieve proposal
				proposal, found, err := signerStorage.RetrieveHighestProposal(tc.account.ValidatorPublicKey())
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, tc.proposal, proposal)
			})
		}

		t.Run("error_nil_pubkey", func(t *testing.T) {
			proposal := phase0.Slot(42)
			err := signerStorage.SaveHighestProposal(nil, proposal)
			require.Error(t, err)
			require.Contains(t, err.Error(), "pubKey must not be nil")

			_, found, err := signerStorage.RetrieveHighestProposal(nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "public key could not be nil")
			require.False(t, found)
		})

		t.Run("error_zero_slot", func(t *testing.T) {
			pubKey := []byte("test_pubkey")
			err := signerStorage.SaveHighestProposal(pubKey, 0)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid proposal slot, slot could not be 0")
		})

		t.Run("EmptyProposalValue", func(t *testing.T) {
			_, signerStorage, done := testWallet(t)
			defer done()

			s := signerStorage.(*storage)
			pubKey := []byte("test_pubkey")

			err := s.db.Set(s.objPrefix(highestProposalPrefix), pubKey, []byte{})
			require.NoError(t, err)

			slot, found, err := signerStorage.RetrieveHighestProposal(pubKey)
			require.Error(t, err)
			require.True(t, found)
			require.Contains(t, err.Error(), "highest proposal value is empty")
			require.Equal(t, phase0.Slot(0), slot)
		})

		t.Run("remove_proposal", func(t *testing.T) {
			account := &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			}

			proposal := phase0.Slot(42)

			err := signerStorage.SaveHighestProposal(account.ValidatorPublicKey(), proposal)
			require.NoError(t, err)

			retrieved, found, err := signerStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, proposal, retrieved)

			err = signerStorage.RemoveHighestProposal(account.ValidatorPublicKey())
			require.NoError(t, err)

			retrieved, found, err = signerStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
			require.NoError(t, err)
			require.False(t, found)
			require.Equal(t, phase0.Slot(0), retrieved)
		})
	})
}

// Helper functions for testing

func _bigInt(input string) *big.Int {
	res, _ := new(big.Int).SetString(input, 10)
	return res
}

type mockAccount struct {
	id            uuid.UUID
	validationKey *big.Int
}

func (a *mockAccount) ID() uuid.UUID    { return a.id }
func (a *mockAccount) Name() string     { return "" }
func (a *mockAccount) BasePath() string { return "" }
func (a *mockAccount) ValidatorPublicKey() []byte {
	sk := &bls.SecretKey{}
	_ = sk.Deserialize(a.validationKey.Bytes())
	return sk.GetPublicKey().Serialize()
}
func (a *mockAccount) WithdrawalPublicKey() []byte                     { return nil }
func (a *mockAccount) ValidationKeySign(data []byte) ([]byte, error)   { return nil, nil }
func (a *mockAccount) GetDepositData() (map[string]interface{}, error) { return nil, nil }
func (a *mockAccount) SetContext(ctx *core.WalletContext)              {}
