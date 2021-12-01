package collections

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"os"
	"path"
	"testing"
	"time"
)

func TestIbftStorage_LoadTesting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dbPath := path.Join(os.TempDir(), "data-testdb")
	db, _ := kv.New(basedb.Options{
		Type:   "badger-db",
		Path:   dbPath,
		Logger: logger,
	})
	defer func(db basedb.IDb) {
		_ = db.RemoveAllByCollection([]byte("attestation"))
		db.Close()
		if err := os.RemoveAll(dbPath); err != nil {
			fmt.Println("failed to remove badger-db in path:", dbPath)
		}
		fmt.Println("cleared all resources")
	}(db)
	storage := NewIbft(db, logger, "attestation")
	n := 10000
	msgs := make([]*proto.SignedMessage, 0)
	for i := 0; i < n; i++ {
		msgs = append(msgs, &proto.SignedMessage{
			Message: &proto.Message{
				Type:      proto.RoundState_Decided,
				Round:     1,
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: uint64(i),
			},
			Signature: []byte{1, 2, 3, 4},
			SignerIds: []uint64{1, 2, 3},
		})
	}
	ts := time.Now()
	require.NoError(t, storage.SaveDecidedMessages(msgs))
	fmt.Printf("time took to save %d", time.Now().Sub(ts).Milliseconds())
	<-time.After(time.Second * 2)
	ts = time.Now()
	inRange, err := storage.GetDecidedInRange([]byte{1, 2, 3, 4}, uint64(0), uint64(n - 1))
	require.NoError(t, err)
	require.Equal(t, n, len(inRange))
	fmt.Printf("time took to read %d", time.Now().Sub(ts).Milliseconds())
}

func TestIbftStorage_SaveDecidedMessages(t *testing.T) {
	storage := NewIbft(newInMemDb(), zap.L(), "attestation")
	n := 1000
	msgs := make([]*proto.SignedMessage, 0)
	for i := 0; i < n; i++ {
		msgs = append(msgs, &proto.SignedMessage{
			Message: &proto.Message{
				Type:      proto.RoundState_Decided,
				Round:     1,
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: uint64(i),
			},
			Signature: []byte{1, 2, 3, 4},
			SignerIds: []uint64{1, 2, 3},
		})
	}
	require.NoError(t, storage.SaveDecidedMessages(msgs))
}

func TestIbftStorage_SaveDecided(t *testing.T) {
	storage := NewIbft(newInMemDb(), zap.L(), "attestation")
	err := storage.SaveDecided(&proto.SignedMessage{
		Message: &proto.Message{
			Type:      proto.RoundState_Decided,
			Round:     2,
			Lambda:    []byte{1, 2, 3, 4},
			SeqNumber: 1,
		},
		Signature: []byte{1, 2, 3, 4},
		SignerIds: []uint64{1, 2, 3},
	})
	require.NoError(t, err)

	value, found, err := storage.GetDecided([]byte{1, 2, 3, 4}, 1)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, []byte{1, 2, 3, 4}, value.Message.Lambda)
	require.EqualValues(t, 1, value.Message.SeqNumber)
	require.EqualValues(t, []byte{1, 2, 3, 4}, value.Signature)

	// not found
	_, found, err = storage.GetDecided([]byte{1, 2, 3, 3}, 1)
	require.NoError(t, err)
	require.False(t, found)
}

func TestIbftStorage_SaveCurrentInstance(t *testing.T) {
	storage := NewIbft(newInMemDb(), zap.L(), "attestation")
	err := storage.SaveCurrentInstance([]byte{1, 2, 3, 4}, &proto.State{
		Stage:         threadsafe.Int32(int32(proto.RoundState_Decided)),
		Lambda:        threadsafe.Bytes(nil),
		SeqNumber:     threadsafe.Uint64(2),
		InputValue:    threadsafe.Bytes(nil),
		Round:         threadsafe.Uint64(0),
		PreparedRound: threadsafe.Uint64(0),
		PreparedValue: threadsafe.Bytes(nil),
	})
	require.NoError(t, err)

	value, _, err := storage.GetCurrentInstance([]byte{1, 2, 3, 4})
	require.NoError(t, err)
	require.EqualValues(t, 2, value.SeqNumber.Get())

	// not found
	_, found, err := storage.GetCurrentInstance([]byte{1, 2, 3, 3})
	require.NoError(t, err)
	require.False(t, found)
}

func TestIbftStorage_GetHighestDecidedInstance(t *testing.T) {
	storage := NewIbft(newInMemDb(), zap.L(), "attestation")
	err := storage.SaveHighestDecidedInstance(&proto.SignedMessage{
		Message: &proto.Message{
			Type:      proto.RoundState_Decided,
			Round:     2,
			Lambda:    []byte{1, 2, 3, 4},
			SeqNumber: 1,
		},
		Signature: []byte{1, 2, 3, 4},
		SignerIds: []uint64{1, 2, 3},
	})
	require.NoError(t, err)

	value, found, err := storage.GetHighestDecidedInstance([]byte{1, 2, 3, 4})
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, []byte{1, 2, 3, 4}, value.Message.Lambda)
	require.EqualValues(t, 1, value.Message.SeqNumber)
	require.EqualValues(t, []byte{1, 2, 3, 4}, value.Signature)

	// not found
	_, found, err = storage.GetHighestDecidedInstance([]byte{1, 2, 3, 3})
	require.NoError(t, err)
	require.False(t, found)
}

func newInMemDb() basedb.IDb {
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	return db
}
