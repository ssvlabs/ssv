package exporter

import (
	"context"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/adapters/mock"
	"github.com/bloxapp/ssv/pubsub"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
)

func TestExporter_ProcessIncomingExportReq(t *testing.T) {
	exp, err := newMockExporter()
	require.NoError(t, err)
	incoming := pubsub.NewSubject()
	cnIn, err := incoming.Register("TestExporter_ProcessIncomingExportReq")
	require.NoError(t, err)
	defer incoming.Deregister("TestExporter_ProcessIncomingExportReq")

	outbound := pubsub.NewSubject()
	cnOut, err := outbound.Register("TestExporter_ProcessIncomingExportReq")
	require.NoError(t, err)
	defer outbound.Deregister("TestExporter_ProcessIncomingExportReq")

	var wg sync.WaitGroup
	go exp.processIncomingExportReq(cnIn, outbound)

	wg.Add(2)
	go func() {
		cnIn <- api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeValidator,
				Filter: api.MessageFilter{From: 0},
			},
			Err:  nil,
			Conn: nil,
		}
		m := <- cnOut
		nm, ok := m.(api.NetworkMessage)
		require.True(t, ok)
		require.Equal(t, api.TypeValidator, nm.Msg.Type)
		wg.Done()

		cnIn <- api.NetworkMessage{
			Msg: api.Message{
				Type:   api.TypeOperator,
				Filter: api.MessageFilter{From: 0},
			},
			Err:  nil,
			Conn: nil,
		}
		m = <- cnOut
		nm, ok = m.(api.NetworkMessage)
		require.True(t, ok)
		require.Equal(t, api.TypeOperator, nm.Msg.Type)
		wg.Done()
	}()

	wg.Wait()
}

func newMockExporter() (*exporter, error) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, err
	}

	adapter := mock.NewAdapterMock(logger)
	ws := api.NewWsServer(logger, adapter, nil)

	opts := Options{
		Ctx:        context.Background(),
		Logger:     logger,
		ETHNetwork: nil,
		Eth1Client: nil,
		Network:    nil,
		DB:         db,
		WS:         ws,
		WsAPIPort:  0,
	}
	e := New(opts)

	return e.(*exporter), nil
}
