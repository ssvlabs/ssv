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
	"runtime"
	"testing"
	"time"
)

type loadTest struct {
	name       string
	write      func([]*proto.SignedMessage)
	read       func([]byte, uint64)
	n          uint64
	identifier []byte
	sleep      time.Duration
}

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

	tests := []loadTest{
		//{
		//	"non-optimized",
		//	func(msgs []*proto.SignedMessage) {
		//		for _, msg := range msgs {
		//			require.NoError(t, storage.SaveDecided(msg))
		//		}
		//	},
		//	func(identifier []byte, n uint64) {
		//		for i := uint64(0); i < n; i++ {
		//			_, found, err := storage.GetDecided(identifier, i)
		//			require.NoError(t, err)
		//			require.True(t, found)
		//		}
		//	},
		//	5000,
		//	[]byte{2, 2, 2, 2},
		//	time.Second * 2,
		//},
		{
			"optimized",
			func(msgs []*proto.SignedMessage) {
				require.NoError(t, storage.SaveDecidedMessages(msgs))
			},
			func(identifier []byte, n uint64) {
				inRange, err := storage.GetDecidedInRange(identifier, uint64(0), n-1)
				require.NoError(t, err)
				require.Equal(t, int(n), len(inRange))
			},
			5000,
			[]byte{1, 1, 1, 1},
			time.Second,
		},
	}

	for i := 1; i < 2; i++ {
		tests = append(tests, loadTest{
			fmt.Sprintf("optimized %d", i),
			func(msgs []*proto.SignedMessage) {
				require.NoError(t, storage.SaveDecidedMessages(msgs))
			},
			func(identifier []byte, n uint64) {
				inRange, err := storage.GetDecidedInRange(identifier, uint64(0), n-1)
				require.NoError(t, err)
				require.Equal(t, int(n), len(inRange))
			},
			5000,
			[]byte(fmt.Sprintf("%d%d%d%d", i, i, i, i)),
			time.Second,
		})
	}

	//tests = append(tests, loadTest{
	//	"optimized 10k",
	//	func(msgs []*proto.SignedMessage) {
	//		require.NoError(t, storage.SaveDecidedMessages(msgs))
	//	},
	//	func(identifier []byte, n uint64) {
	//		inRange, err := storage.GetDecidedInRange(identifier, uint64(0), n-1)
	//		require.NoError(t, err)
	//		require.Equal(t, int(n), len(inRange))
	//	},
	//	10000,
	//	[]byte{1, 0, 0, 0},
	//	time.Second,
	//})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			<-time.After(test.sleep)
			msgs := make([]*proto.SignedMessage, 0)
			for i := uint64(0); i < test.n; i++ {
				msgs = append(msgs, &proto.SignedMessage{
					Message: &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    test.identifier[:],
						SeqNumber: i,
					},
					Signature: []byte{1, 2, 3, 4},
					SignerIds: []uint64{1, 2, 3},
				})
			}
			reportRuntimeStats(logger)
			ts := time.Now()
			test.write(msgs)
			logger.Debug(fmt.Sprintf("[%s] time took to save %dms for %d items",
				test.name, time.Since(ts).Milliseconds(), test.n))
			reportRuntimeStats(logger)
			<-time.After(test.sleep)
			reportRuntimeStats(logger)
			ts = time.Now()
			test.read(test.identifier, test.n)
			logger.Debug(fmt.Sprintf("[%s] time took to read %dms for %d items",
				test.name, time.Since(ts).Milliseconds(), test.n))
			reportRuntimeStats(logger)
		})
	}
}

func reportRuntimeStats(logger *zap.Logger) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	logger.With(zap.String("who", "reportRuntimeStats")).Debug("mem stats",
		zap.Uint64("mem.Alloc", mem.Alloc),
		zap.Uint64("mem.TotalAlloc", mem.TotalAlloc),
		zap.Uint64("mem.HeapAlloc", mem.HeapAlloc),
		zap.Uint32("mem.NumGC", mem.NumGC),
	)
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
