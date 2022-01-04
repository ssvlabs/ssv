package controller

import (
	"encoding/binary"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"runtime"
	"sync"
	"testing"
	"time"
)

type reader struct {
	db basedb.IDb
}

func init() {
	logex.Build("", zapcore.DebugLevel, nil)
}

func (r reader) GetMsgResolver(networkMsg network.NetworkMsg) func(msg *proto.SignedMessage) {
	return func(msg *proto.SignedMessage) {
		seq := make([]byte, 8)
		binary.LittleEndian.PutUint64(seq, msg.Message.SeqNumber)
		if err := r.db.Set([]byte("test"), seq, []byte("val")); err != nil {
			panic(fmt.Errorf("faild to set value - %s", err))
		}
	}
}

func TestMediator_AddListener(t *testing.T) {
	db, err := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: logex.GetLogger(),
	})
	require.NoError(t, err)

	mediator := NewMediator(logex.GetLogger())
	reader := &reader{db: db}

	cn := make(chan *proto.SignedMessage)
	mediator.AddListener(network.NetworkMsg_DecidedType, cn, func() {
		close(cn)
	}, func(publicKey string) (MediatorReader, bool) {
		return reader, true
	})

	for i := 1; i < 5; i++ {
		cn <- &proto.SignedMessage{
			Message: &proto.Message{
				Type:      0,
				Round:     0,
				Lambda:    nil,
				SeqNumber: uint64(i),
				Value:     nil,
			},
			Signature: nil,
			SignerIds: nil,
		}
	}

	time.Sleep(time.Millisecond * 100)
	var objs []basedb.Obj
	require.NoError(t, db.GetAll([]byte("test"), func(i int, obj basedb.Obj) error {
		objs = append(objs, obj)
		require.Equal(t, uint64(i+1), binary.LittleEndian.Uint64(obj.Key))
		return nil
	}))
	require.Equal(t, 4, len(objs))
}

func TestNewMediator(t *testing.T) {
	ch := make(chan int, 6000)

	var wg sync.WaitGroup

	go func() {
		for i := range ch {
			wg.Done()
			fmt.Printf("index %d with goroutines: %d\n", i, runtime.NumGoroutine())
			time.Sleep(time.Millisecond * 50)
		}
	}()

	wg.Add(3000)

	go func() {
		for i := 1; i <= 3000; i++ {
			go func(i int) {
				ch <- i
			}(i)
		}
	}()

	wg.Wait()
	fmt.Printf("#goroutines AT END: %d\n", runtime.NumGoroutine())
}
