package ekm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/encryptor/keystorev4"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/google/uuid"
	"github.com/herumi/bls-eth-go-binary/bls"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"testing"
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

func getStorage(t *testing.T) basedb.IDb {
	options := basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	}
	db, err := storage.GetStorageFactory(options)
	require.NoError(t, err)
	return db
}

func getWalletStorage(t *testing.T) *signerStorage {
	return newSignerStorage(getStorage(t), core.PraterNetwork)
}

func testWallet(t *testing.T) (core.Wallet, *signerStorage) {
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
	defer storage.db.Close()
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
	defer storage.db.Close()

	accts, err := storage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accts, 1)

	require.NoError(t, storage.DeleteAccount(accts[0].ID()))
	acc, err := storage.OpenAccount(accts[0].ID())
	require.EqualError(t, err, "could not find account")
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
			defer storage.db.Close()

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

/**
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

func testBlock(t *testing.T) *eth.BeaconBlock {
	blockByts := "7b22736c6f74223a312c2270726f706f7365725f696e646578223a38352c22706172656e745f726f6f74223a224f6b4f6b767962375755666f43634879543333476858794b7741666c4e64534f626b374b49534c396432733d222c2273746174655f726f6f74223a227264584c666d704c2f396a4f662b6c7065753152466d4747486a4571315562633955674257576d505236553d222c22626f6479223a7b2272616e64616f5f72657665616c223a226f734657704c79554f664859583549764b727170626f4d5048464a684153456232333057394b32556b4b41774c38577473496e41573138572f555a5a597652384250777267616c4e45316f48775745397468555277584b4574522b767135684f56744e424868626b5831426f3855625a51532b5230787177386a667177396446222c22657468315f64617461223a7b226465706f7369745f726f6f74223a22704f564553434e6d764a31546876484e444576344e7a4a324257494c39417856464e55642f4b3352536b6f3d222c226465706f7369745f636f756e74223a3132382c22626c6f636b5f68617368223a22704f564553434e6d764a31546876484e444576344e7a4a324257494c39417856464e55642f4b3352536b6f3d227d2c226772616666697469223a22414141414141414141414141414141414141414141414141414141414141414141414141414141414141413d222c2270726f706f7365725f736c617368696e6773223a6e756c6c2c2261747465737465725f736c617368696e6773223a6e756c6c2c226174746573746174696f6e73223a5b7b226167677265676174696f6e5f62697473223a2248773d3d222c2264617461223a7b22736c6f74223a302c22636f6d6d69747465655f696e646578223a302c22626561636f6e5f626c6f636b5f726f6f74223a224f6b4f6b767962375755666f43634879543333476858794b7741666c4e64534f626b374b49534c396432733d222c22736f75726365223a7b2265706f6368223a302c22726f6f74223a22414141414141414141414141414141414141414141414141414141414141414141414141414141414141413d227d2c22746172676574223a7b2265706f6368223a302c22726f6f74223a224f6b4f6b767962375755666f43634879543333476858794b7741666c4e64534f626b374b49534c396432733d227d7d2c227369676e6174757265223a226c37627963617732537751633147587a4c36662f6f5a39616752386562685278503550675a546676676e30344b367879384a6b4c68506738326276674269675641674347767965357a7446797a4772646936555a655a4850593030595a6d3964513939764352674d34676f31666b3046736e684543654d68522f45454b59626a227d5d2c226465706f73697473223a6e756c6c2c22766f6c756e746172795f6578697473223a6e756c6c7d7d"
	blk := &eth.BeaconBlock{}
	require.NoError(t, json.Unmarshal(_byteArray(blockByts), blk))
	return blk
}

func TestSavingProposal(t *testing.T) {
	_, storage := testWallet(t)
	defer storage.db.Close()

	tests := []struct {
		name     string
		proposal *eth.BeaconBlock
		account  core.ValidatorAccount
	}{
		{
			name:     "simple save",
			proposal: testBlock(t),
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// save
			err := storage.SaveHighestProposal(test.account.ValidatorPublicKey(), test.proposal)
			require.NoError(t, err)

			// fetch
			proposal := storage.RetrieveHighestProposal(test.account.ValidatorPublicKey())
			require.NotNil(t, proposal)

			// test equal
			aRoot, err := proposal.HashTreeRoot()
			require.NoError(t, err)
			bRoot, err := proposal.HashTreeRoot()
			require.NoError(t, err)
			require.EqualValues(t, aRoot, bRoot)
		})
	}
}

func TestSavingAttestation(t *testing.T) {
	_, storage := testWallet(t)
	defer storage.db.Close()

	tests := []struct {
		name    string
		att     *eth.AttestationData
		account core.ValidatorAccount
	}{
		{
			name: "simple save",
			att: &eth.AttestationData{
				Slot:            30,
				CommitteeIndex:  1,
				BeaconBlockRoot: make([]byte, 32),
				Source: &eth.Checkpoint{
					Epoch: 1,
					Root:  make([]byte, 32),
				},
				Target: &eth.Checkpoint{
					Epoch: 4,
					Root:  make([]byte, 32),
				},
			},
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
		{
			name: "simple save with no change to latest attestation target",
			att: &eth.AttestationData{
				Slot:            30,
				CommitteeIndex:  1,
				BeaconBlockRoot: make([]byte, 32),
				Source: &eth.Checkpoint{
					Epoch: 1,
					Root:  make([]byte, 32),
				},
				Target: &eth.Checkpoint{
					Epoch: 3,
					Root:  make([]byte, 32),
				},
			},
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// save
			err := storage.SaveHighestAttestation(test.account.ValidatorPublicKey(), test.att)
			require.NoError(t, err)

			// fetch
			att := storage.RetrieveHighestAttestation(test.account.ValidatorPublicKey())
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
	_, storage := testWallet(t)
	defer storage.db.Close()

	tests := []struct {
		name    string
		att     *eth.AttestationData
		account core.ValidatorAccount
	}{
		{
			name: "simple save",
			att: &eth.AttestationData{
				Slot:            30,
				CommitteeIndex:  1,
				BeaconBlockRoot: make([]byte, 32),
				Source: &eth.Checkpoint{
					Epoch: 1,
					Root:  make([]byte, 32),
				},
				Target: &eth.Checkpoint{
					Epoch: 4,
					Root:  make([]byte, 32),
				},
			},
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
		{
			name: "simple save with no change to latest attestation target",
			att: &eth.AttestationData{
				Slot:            30,
				CommitteeIndex:  1,
				BeaconBlockRoot: make([]byte, 32),
				Source: &eth.Checkpoint{
					Epoch: 1,
					Root:  make([]byte, 32),
				},
				Target: &eth.Checkpoint{
					Epoch: 3,
					Root:  make([]byte, 32),
				},
			},
			account: &mockAccount{
				id:            uuid.New(),
				validationKey: _bigInt("5467048590701165350380985526996487573957450279098876378395441669247373404218"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// save
			err := storage.SaveHighestAttestation(test.account.ValidatorPublicKey(), test.att)
			require.NoError(t, err)

			// fetch
			att := storage.RetrieveHighestAttestation(test.account.ValidatorPublicKey())
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
