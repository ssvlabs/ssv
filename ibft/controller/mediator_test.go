package controller

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
		if err := r.db.Set([]byte("test"), []byte("key"), []byte("val")); err != nil {
			panic(fmt.Errorf("faild to set value - %s", err))
		}
	}
}

func TestRedirect(t *testing.T) {
	db, err := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	require.NoError(t, err)

	msg := proto.SignedMessage{
		Message: &proto.Message{
			Type:                 0,
			Round:                0,
			Lambda:               nil,
			SeqNumber:            0,
			Value:                nil,
			XXX_NoUnkeyedLiteral: struct{}{},
			XXX_unrecognized:     nil,
			XXX_sizecache:        0,
		},
		Signature:            nil,
		SignerIds:            nil,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_unrecognized:     nil,
		XXX_sizecache:        0,
	}

	mediator := NewMediator(logex.GetLogger())
	reader := &reader{db: db}

	mediator.Redirect(func(publicKey string) (MediatorReader, bool) {
		return reader, true
	}, &msg)

	time.Sleep(time.Second * 2)
	obj, ok, err := db.Get([]byte("test"), []byte("key"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("val"), obj.Value)
}
