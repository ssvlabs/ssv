package collections

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

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
