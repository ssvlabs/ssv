package commons

import (
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/tasks"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// WaitMinPeersCtx represents the context needed for WaitForMinPeers
type WaitMinPeersCtx struct {
	Ctx       context.Context
	Logger    *zap.Logger
	Net       network.OperatorsDiscoverer
	Operators validatorstorage.OperatorsPubKeys
}

// WaitForMinPeers waits until min peers joined the validator's topic
func WaitForMinPeers(ctx WaitMinPeersCtx, validatorPk []byte, min int, start, limit time.Duration, stopAtLimit bool) error {
	interval := start
	q := tasks.NewExecutionQueue(1 * time.Millisecond)
	go q.Start()
	defer q.Stop()

	findPeers := func() error {
		if len(ctx.Operators) > 0 {
			c, cancel := context.WithTimeout(ctx.Ctx, 30*time.Second)
			defer cancel()
			ctx.Net.FindPeers(c, ctx.Operators...)
		}
		return nil
	}

	for {
		ok, n := haveMinPeers(ctx.Logger, ctx.Net, validatorPk, min)
		if ok {
			ctx.Logger.Info("found enough peers",
				zap.Int("current peer count", n))
			if n == min {
				// still calling find peers as the minimum is usually not sufficient
				// (min - 1, but things won't start w/o additional peers)
				q.QueueDistinct(findPeers, "find-peers")
			}
			break
		}
		ctx.Logger.Info("waiting for min peers",
			zap.Int("current peer count", n))

		q.QueueDistinct(findPeers, "find-peers")

		time.Sleep(interval)

		select {
		case <-ctx.Ctx.Done():
			return errors.New("timed out")
		default:
			interval *= 2
			if stopAtLimit && interval == limit {
				return errors.New("could not find peers")
			}
			interval %= limit
			if interval == 0 {
				interval = start
			}
		}
	}
	return nil
}

// haveMinPeers checks that there are at least <count> connected peers
func haveMinPeers(logger *zap.Logger, net network.OperatorsDiscoverer, validatorPk []byte, count int) (bool, int) {
	peers, err := net.AllPeers(validatorPk)
	if err != nil {
		logger.Error("failed fetching peers", zap.Error(err))
		return false, 0
	}
	nPeers := len(peers)
	return nPeers >= count, nPeers
}
