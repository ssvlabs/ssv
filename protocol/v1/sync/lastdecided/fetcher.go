package lastdecided

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/sync"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// GetLastDecided reads last decided message from store
type GetLastDecided func(i message.Identifier) (*message.SignedMessage, error)

// Fetcher is responsible for fetching last/highest decided messages from other peers in the network
type Fetcher interface {
	GetLastDecided(ctx context.Context, identifier message.Identifier, getLastDecided GetLastDecided) (*message.SignedMessage, string, message.Height, error)
}

type lastDecidedFetcher struct {
	logger *zap.Logger
	syncer p2pprotocol.Syncer
}

// NewLastDecidedFetcher creates a new instance of fetcher
func NewLastDecidedFetcher(logger *zap.Logger, syncer p2pprotocol.Syncer) Fetcher {
	return &lastDecidedFetcher{
		logger: logger,
		syncer: syncer,
	}
}

// GetLastDecided returns last decided message from other peers in the network
func (l *lastDecidedFetcher) GetLastDecided(ctx context.Context, identifier message.Identifier, getLastDecided GetLastDecided) (*message.SignedMessage, string, message.Height, error) {
	logger := l.logger.With(zap.String("identifier", identifier.String()))
	var err error
	var remoteMsgs []p2pprotocol.SyncResult
	retries := 2
	for retries > 0 && len(remoteMsgs) == 0 {
		retries--
		remoteMsgs, err = l.syncer.LastDecided(identifier)
		if err != nil {
			return nil, "", 0, errors.Wrap(err, "could not get local highest decided")
		}
		if len(remoteMsgs) == 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if len(remoteMsgs) == 0 {
		logger.Info("node is synced: remote highest decided not found (V0), assuming 0")
		return nil, "", 0, nil
	}

	localMsg, err := getLastDecided(identifier)
	if err != nil {
		return nil, "", 0, errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	var localHeight message.Height
	if localMsg != nil {
		localHeight = localMsg.Message.Height
	}

	highest, height, sender := sync.GetHighest(l.logger, localMsg, remoteMsgs...)
	if highest == nil {
		logger.Info("node is synced: remote highest decided not found (V1)")
		return nil, "", localHeight, nil
	}

	if height <= localHeight {
		logger.Info("node is synced: local is higher or equal to remote")
		return nil, "", height, nil
	}

	return highest, sender, localHeight, nil
}
