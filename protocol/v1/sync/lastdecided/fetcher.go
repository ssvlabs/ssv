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

const (
	lastDecidedRetries  = 8
	lastDecidedInterval = 250 * time.Millisecond
	lastDecidedTimeout  = 25 * time.Second
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
func (l *lastDecidedFetcher) GetLastDecided(pctx context.Context, identifier message.Identifier, getLastDecided GetLastDecided) (*message.SignedMessage, string, message.Height, error) {
	ctx, cancel := context.WithTimeout(pctx, lastDecidedTimeout)
	defer cancel()
	var err error
	var sender string
	var remoteMsgs []p2pprotocol.SyncResult
	var localMsg, highest *message.SignedMessage

	logger := l.logger.With(zap.String("identifier", identifier.String()))

	retries := lastDecidedRetries
	// TODO: use exponent interval?
	for retries > 0 && len(remoteMsgs) == 0 && ctx.Err() == nil {
		retries--
		remoteMsgs, err = l.syncer.LastDecided(identifier)
		if err != nil {
			// if network is not ready yet, wait some more
			if err == p2pprotocol.ErrNetworkIsNotReady {
				time.Sleep(lastDecidedInterval * 2)
				continue
			}
			l.logger.Debug("could not get highest decided from remote peers", zap.Error(err))
		}
		if len(remoteMsgs) == 0 {
			time.Sleep(lastDecidedInterval)
		}

		highest, sender = sync.GetHighest(l.logger, remoteMsgs...)
		if highest == nil {
			continue
		}
	}

	var localHeight message.Height
	localMsg, err = getLastDecided(identifier)
	if err != nil {
		return nil, "", 0, errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	if localMsg != nil && localMsg.Message != nil {
		localHeight = localMsg.Message.Height
	}
	logger = logger.With(zap.Int64("localHeight", int64(localHeight)))
	// couldn't fetch highest from remote peers
	if highest == nil || highest.Message == nil {
		if localMsg == nil {
			// couldn't find local highest decided -> height is 0
			logger.Debug("node is synced: local and remote highest decided not found, assuming 0")
			return nil, "", 0, nil
		}
		// local was found while remote didn't
		logger.Debug("node is synced: remote highest decided not found")
		return nil, "", localHeight, nil
	}

	if highest.Message.Height <= localHeight {
		logger.Debug("node is synced: local is higher or equal to remote",
			zap.Int64("remoteHeight", int64(highest.Message.Height)))
		return nil, "", localHeight, nil
	}

	logger.Debug("fetched last decided from remote peer",
		zap.Int64("remoteHeight", int64(highest.Message.Height)), zap.String("remotePeer", sender))

	return highest, sender, localHeight, nil
}
