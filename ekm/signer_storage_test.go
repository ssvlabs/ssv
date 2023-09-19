package ekm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/encryptor/keystorev4"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	"github.com/google/uuid"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/threshold"
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

func getBaseStorage(logger *zap.Logger) (basedb.Database, error) {
	return kv.NewInMemory(context.TODO(), logger, basedb.Options{})
}

func newStorageForTest(t *testing.T) (Storage, func()) {
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	spDB, err := getBaseStorage(logger)
	require.NoError(t, err)

	s := NewEKMStorage(db, spDB, networkconfig.TestNetwork.Beacon.GetBeaconNetwork(), logger)
	return s, func() {
		db.Close()
	}
}

func testWallet(t *testing.T) (core.Wallet, Storage, func()) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()
	index := 1

	//signerStorage := getWalletStorage(t)
	signerStorage, done := newStorageForTest(t)

	wallet := hd.NewWallet(&core.WalletContext{Storage: signerStorage})
	require.NoError(t, signerStorage.SaveWallet(wallet))

	_, err := wallet.CreateValidatorAccountFromPrivateKey(sk.Serialize(), &index)
	require.NoError(t, err)

	return wallet, signerStorage, done
}

func TestOpeningAccounts(t *testing.T) {
	wallet, _, done := testWallet(t)
	defer done()
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
	_, signerStorage, done := testWallet(t)
	defer done()

	accts, err := signerStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accts, 1)

	require.NoError(t, signerStorage.DeleteAccount(accts[0].ID()))
	acc, err := signerStorage.OpenAccount(accts[0].ID())
	require.EqualError(t, err, "account not found")
	require.Nil(t, acc)
}

func TestNonExistingWallet(t *testing.T) {
	signerStorage, done := newStorageForTest(t)
	defer done()

	w, err := signerStorage.OpenWallet()
	require.NotNil(t, err)
	require.EqualError(t, err, "could not find wallet")
	require.Nil(t, w)
}

func TestNonExistingAccount(t *testing.T) {
	wallet, _, done := testWallet(t)
	defer done()

	account, err := wallet.AccountByID(uuid.New())
	require.EqualError(t, err, "account not found")
	require.Nil(t, account)
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
		test := test
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
					require.Equal(t, test.error.Error(), err.Error())
				} else {
					t.Error(err)
				}
				return
			}

			// fetch wallet by id
			fetched, err := signerStorage.OpenWallet()
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

/*
*
slashing store tests
*/
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

func testSlot() phase0.Slot {
	return phase0.Slot(1)
}

func TestSavingProposal(t *testing.T) {
	_, signerStorage, done := testWallet(t)
	defer done()

	tests := []struct {
		name     string
		proposal phase0.Slot
		account  core.ValidatorAccount
	}{
		{
			name:     "simple save",
			proposal: testSlot(),
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			// save
			err := signerStorage.SaveHighestProposal(test.account.ValidatorPublicKey(), test.proposal)
			require.NoError(t, err)

			// fetch
			proposal, found, err := signerStorage.RetrieveHighestProposal(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, proposal)

			// test equal
			require.EqualValues(t, test.proposal, proposal)
		})
	}
}

func TestSavingAttestation(t *testing.T) {
	_, signerStorage, done := testWallet(t)
	defer done()

	tests := []struct {
		name    string
		att     *phase0.AttestationData
		account core.ValidatorAccount
	}{
		{
			name: "simple save",
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
			name: "simple save with no change to latest attestation target",
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

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			// save
			err := signerStorage.SaveHighestAttestation(test.account.ValidatorPublicKey(), test.att)
			require.NoError(t, err)

			// fetch
			att, found, err := signerStorage.RetrieveHighestAttestation(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, att)

			// test equal
			aRoot, err := att.HashTreeRoot()
			require.NoError(t, err)
			bRoot, err := test.att.HashTreeRoot()
			require.NoError(t, err)
			require.EqualValues(t, aRoot, bRoot)
		})
	}
}

func TestSavingHighestAttestation(t *testing.T) {
	_, signerStorage, done := testWallet(t)
	defer done()

	tests := []struct {
		name    string
		att     *phase0.AttestationData
		account core.ValidatorAccount
	}{
		{
			name: "simple save",
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
			name: "simple save with no change to latest attestation target",
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

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			// save
			err := signerStorage.SaveHighestAttestation(test.account.ValidatorPublicKey(), test.att)
			require.NoError(t, err)

			// fetch
			att, found, err := signerStorage.RetrieveHighestAttestation(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, att)

			// test equal
			aRoot, err := att.HashTreeRoot()
			require.NoError(t, err)
			bRoot, err := test.att.HashTreeRoot()
			require.NoError(t, err)
			require.EqualValues(t, aRoot, bRoot)
		})
	}
}

func TestRemovingHighestAttestation(t *testing.T) {
	_, signerStorage, done := testWallet(t)
	defer done()

	tests := []struct {
		name    string
		att     *phase0.AttestationData
		account core.ValidatorAccount
	}{
		{
			name: "remove highest attestation",
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
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			// save
			err := signerStorage.SaveHighestAttestation(test.account.ValidatorPublicKey(), test.att)
			require.NoError(t, err)

			// fetch
			att, found, err := signerStorage.RetrieveHighestAttestation(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, att)

			// test equal
			aRoot, err := att.HashTreeRoot()
			require.NoError(t, err)
			bRoot, err := test.att.HashTreeRoot()
			require.NoError(t, err)
			require.EqualValues(t, aRoot, bRoot)

			// remove
			err = signerStorage.RemoveHighestAttestation(test.account.ValidatorPublicKey())
			require.NoError(t, err)

			// fetch
			att, found, err = signerStorage.RetrieveHighestAttestation(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.False(t, found)
			require.Nil(t, att)
		})
	}
}

func TestRemovingHighestProposal(t *testing.T) {
	_, signerStorage, done := testWallet(t)
	defer done()

	tests := []struct {
		name     string
		proposal phase0.Slot
		account  core.ValidatorAccount
	}{
		{
			name:     "remove highest proposal",
			proposal: testSlot(),
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			// save
			err := signerStorage.SaveHighestProposal(test.account.ValidatorPublicKey(), test.proposal)
			require.NoError(t, err)

			// fetch
			proposal, found, err := signerStorage.RetrieveHighestProposal(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, proposal)

			// test equal
			require.EqualValues(t, test.proposal, proposal)

			// remove
			err = signerStorage.RemoveHighestProposal(test.account.ValidatorPublicKey())
			require.NoError(t, err)

			// fetch
			proposal, found, err = signerStorage.RetrieveHighestProposal(test.account.ValidatorPublicKey())
			require.NoError(t, err)
			require.False(t, found)
			require.Equal(t, phase0.Slot(0), proposal)
		})
	}
}
