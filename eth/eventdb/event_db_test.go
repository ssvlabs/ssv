package eventdb

import (
	"context"
	"math/big"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var (
	testKey, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddress = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestEventDBLastBlock(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := NewEventDB(db.Badger())
	txnW := eventDB.RWTxn()
	defer txnW.Discard()

	err = txnW.SetLastProcessedBlock(big.NewInt(1001))
	require.NoError(t, err)
	err = txnW.Commit()
	require.NoError(t, err)

	txnR := eventDB.ROTxn()
	defer txnR.Discard()
	lastBlock, err := txnR.GetLastProcessedBlock()
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1001), lastBlock)
}

func TestEventDBOperatorData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := NewEventDB(db.Badger())
	txnW := eventDB.RWTxn()
	defer txnW.Discard()
	dataToSave := storage.OperatorData{ID: 1, PublicKey: crypto.CompressPubkey(&testKey.PublicKey), OwnerAddress: testAddress}
	_, err = txnW.SaveOperatorData(&dataToSave)
	require.NoError(t, err)
	err = txnW.Commit()
	require.NoError(t, err)

	txnR := eventDB.ROTxn()
	defer txnR.Discard()
	data, err := txnR.GetOperatorData(1)
	require.NoError(t, err)
	require.Equal(t, &dataToSave, data)
}

func TestEventDBRecipientData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := NewEventDB(db.Badger())
	txnW := eventDB.RWTxn()
	defer txnW.Discard()
	nonce := registrystorage.Nonce(0)
	dataToSave := registrystorage.RecipientData{Owner: testAddress, FeeRecipient: bellatrix.ExecutionAddress(testAddress), Nonce: &nonce}
	_, err = txnW.SaveRecipientData(&dataToSave)
	require.NoError(t, err)
	err = txnW.Commit()
	require.NoError(t, err)

	txnR := eventDB.ROTxn()
	defer txnR.Discard()
	data, err := txnR.GetRecipientData(testAddress)
	require.NoError(t, err)
	require.Equal(t, &dataToSave, data)
}

func TestEventDBShares(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := NewEventDB(db.Badger())
	txnW := eventDB.RWTxn()
	defer txnW.Discard()

	dataToSave := types.SSVShare{
		Share: spectypes.Share{ValidatorPubKey: crypto.CompressPubkey(&testKey.PublicKey), OperatorID: 1},
		Metadata: types.Metadata{
			BeaconMetadata: &beacon.ValidatorMetadata{
				Index: phase0.ValidatorIndex(1),
			},
			OwnerAddress: testAddress,
			Liquidated:   false,
		},
	}
	err = txnW.SaveShares(&dataToSave)
	require.NoError(t, err)
	err = txnW.Commit()
	require.NoError(t, err)
}

func TestEventDBBumpNonce(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Ctx:       ctx,
	}

	db, err := kv.New(logger, options)
	require.NoError(t, err)

	eventDB := NewEventDB(db.Badger())
	txnW := eventDB.RWTxn()
	defer txnW.Discard()
	nonce := registrystorage.Nonce(0)
	err = txnW.BumpNonce(testAddress)
	require.NoError(t, err)
	err = txnW.Commit()
	require.NoError(t, err)

	txnR := eventDB.ROTxn()
	defer txnR.Discard()
	newNonce, err := txnR.GetNextNonce(testAddress)
	require.NoError(t, err)
	require.Equal(t, newNonce, nonce + 1)
}