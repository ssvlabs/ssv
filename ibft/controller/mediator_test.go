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
	objs, err := db.GetAllByCollection([]byte("test"))
	require.NoError(t, err)
	require.Equal(t, 4, len(objs))
	require.Equal(t, uint64(1), binary.LittleEndian.Uint64(objs[0].Key))
	require.Equal(t, uint64(4), binary.LittleEndian.Uint64(objs[3].Key))
}
