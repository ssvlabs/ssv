package node

import (
	"context"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/pubsub"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"testing"
	"time"
)

func TestSyncEth1(t *testing.T) {
	_, _, eth1Client, ssvNode := setupNodeWithEth1ClientMock()

	rawOffset := DefaultSyncOffset().Uint64()
	rawOffset += 2
	go func() {
		logs := []types.Log{types.Log{}, types.Log{BlockNumber: rawOffset}}
		eth1Client.sub.Notify(eth1.Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.sub.Notify(eth1.Event{Data: struct{}{}, Log: logs[1]})
		eth1Client.sub.Notify(eth1.Event{Data: eth1.SyncEndedEvent{Logs: logs, Parsed: 2}})
	}()
	err := ssvNode.syncEth1()
	require.NoError(t, err)

	syncOffset, err := ssvNode.storage.GetSyncOffset()
	require.NoError(t, err)
	require.Equal(t, syncOffset.Uint64(), rawOffset)
}

func TestFailedSyncEth1(t *testing.T) {
	_, _, eth1Client, ssvNode := setupNodeWithEth1ClientMock()
	eth1Client.syncResponse = errors.New("eth1-sync-test")
	go func() {
		logs := []types.Log{types.Log{}, types.Log{BlockNumber: DefaultSyncOffset().Uint64()}}
		eth1Client.sub.Notify(eth1.Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.sub.Notify(eth1.Event{Data: struct{}{}, Log: logs[1]})
		// marking 1 event as failed
		eth1Client.sub.Notify(eth1.Event{Data: eth1.SyncEndedEvent{Logs: logs, Parsed: 1}})
	}()
	err := ssvNode.syncEth1()
	require.EqualError(t, err, "failed to sync contract events: eth1-sync-test")

	_, err = ssvNode.storage.GetSyncOffset()
	require.NotNil(t, err)
}

func setupNodeWithEth1ClientMock() (basedb.IDb, *zap.Logger, *eth1ClientMock, *ssvNode) {
	logger := zap.L()
	db, _ := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	sub := pubsub.NewSubject()
	eth1Client := eth1ClientMock{sub, 10 * time.Millisecond, nil}
	node := ssvNode{
		context:    context.TODO(),
		logger:     logger,
		storage:    NewSSVNodeStorage(db, logger),
		eth1Client: &eth1Client,
	}
	return db, logger, &eth1Client, &node
}

type eth1ClientMock struct {
	sub pubsub.Subject

	syncTimeout  time.Duration
	syncResponse error
}

func (ec *eth1ClientMock) Subject() pubsub.Subscriber {
	return ec.sub
}

func (ec *eth1ClientMock) Start() error {
	return nil
}

func (ec *eth1ClientMock) Sync(fromBlock *big.Int) error {
	time.Sleep(ec.syncTimeout)
	return ec.syncResponse
}
