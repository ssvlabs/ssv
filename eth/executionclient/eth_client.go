package executionclient

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ethClient is a wrapper around ethclient.Client to add a default timeout for every call we
// make with ethclient.Client. This allows us to manage timeouts for ethclient.Client in one
// place such that we don't accidentally forget to set a timeout for some ethclient.Client call.
type ethClient struct {
	client *ethclient.Client

	// reqTimeout specifies the default timeout to be used with every ethclient.Client call.
	reqTimeout time.Duration
}

func newEthClient(client *ethclient.Client, reqTimeout time.Duration) *ethClient {
	return &ethClient{
		client:     client,
		reqTimeout: reqTimeout,
	}
}

func (c *ethClient) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.SyncProgress(reqCtx)
}

func (c *ethClient) BlockNumber(ctx context.Context) (uint64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.BlockNumber(reqCtx)
}

func (c *ethClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.HeaderByNumber(reqCtx, blockNumber)
}

func (c *ethClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.SubscribeFilterLogs(reqCtx, q, ch)
}

func (c *ethClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.FilterLogs(reqCtx, q)
}

func (c *ethClient) SubscribeNewHead(ctx context.Context, heads chan *ethtypes.Header) (ethereum.Subscription, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.SubscribeNewHead(reqCtx, heads)
}

func (c *ethClient) ChainID(ctx context.Context) (*big.Int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.ChainID(reqCtx)
}

func (c *ethClient) Close() {
	c.client.Close()
}
