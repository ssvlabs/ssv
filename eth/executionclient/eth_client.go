package executionclient

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ethClient wraps two ethclient.Client instances and routes each call to the
// transport best suited for it:
//   - sub:   used only for streaming endpoints (eth_subscribe family). These
//            require WebSocket or IPC and only carry tiny messages, so they
//            don't trigger response-side backpressure on any EL.
//   - query: used for request/response endpoints (eth_getLogs, eth_blockNumber,
//            etc.). These can return large payloads; routing them over HTTP
//            avoids Besu's WebSocket-only StreamBackpressure code path that
//            parks the JSON-RPC event loop on large responses
//            (see hyperledger/besu#9848 + WebSocketMessageHandler.replyToClient).
//
// When the dual-transport configuration isn't set (queryClient is nil), every
// call falls back to subClient — preserving the pre-split single-client
// behavior and keeping the change backward compatible.
type ethClient struct {
	sub      *ethclient.Client
	subAddr  string // URL dialed for sub (always set)
	query    *ethclient.Client
	queryAddr string // URL dialed for query (empty when query is nil)

	// reqTimeout specifies the default timeout to be used with every ethclient.Client call.
	reqTimeout time.Duration
}

func newEthClient(sub *ethclient.Client, subAddr string, query *ethclient.Client, queryAddr string, reqTimeout time.Duration) *ethClient {
	return &ethClient{
		sub:        sub,
		subAddr:    subAddr,
		query:      query,
		queryAddr:  queryAddr,
		reqTimeout: reqTimeout,
	}
}

// queryOrSub returns the query client if configured, otherwise falls back to
// the subscription client. This keeps single-transport deployments working
// unchanged.
func (c *ethClient) queryOrSub() *ethclient.Client {
	if c.query != nil {
		return c.query
	}
	return c.sub
}

// SubAddr returns the URL of the subscription transport (the address dialed
// for eth_subscribe — always WS or IPC).
func (c *ethClient) SubAddr() string {
	return c.subAddr
}

// QueryAddr returns the URL that actually serves query-routed calls
// (FilterLogs, BlockNumber, HeaderByNumber, SyncProgress, ChainID): the
// query client's address when dual-transport is configured, the sub client's
// address otherwise. Use this when recording observability for query-routed
// calls so logs/metrics report the URL the request truly traveled.
func (c *ethClient) QueryAddr() string {
	if c.query != nil {
		return c.queryAddr
	}
	return c.subAddr
}

// QueryTransport returns "http" when dual-transport is configured (query
// client present) and "ws" otherwise. Pair with QueryAddr() so observability
// records can be filtered by transport regardless of operator URL choice.
func (c *ethClient) QueryTransport() string {
	if c.query == nil {
		return "ws"
	}
	return "http"
}

func (c *ethClient) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.queryOrSub().SyncProgress(reqCtx)
}

// PingSub performs a cheap round-trip against the subscription transport only.
// In single-transport mode (query == nil) this is a no-op — SyncProgress on
// the sub client itself already covers liveness. When dual-transport is in
// use, Healthy()'s SyncProgress probe only touches the query (HTTP) client,
// so without this probe a WS-down / HTTP-up partial failure would report
// healthy while SubscribeFilterLogs / SubscribeNewHead silently stall, and
// MultiClient would never failover.
//
// Uses BlockNumber as the probe: tiny response (a uint64), can't trigger
// the Besu WS-backpressure path we're avoiding for queries.
func (c *ethClient) PingSub(ctx context.Context) error {
	if c.query == nil {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()
	_, err := c.sub.BlockNumber(reqCtx)
	return err
}

func (c *ethClient) BlockNumber(ctx context.Context) (uint64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.queryOrSub().BlockNumber(reqCtx)
}

func (c *ethClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.queryOrSub().HeaderByNumber(reqCtx, blockNumber)
}

func (c *ethClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	// Always uses the subscription transport — eth_subscribe is unsupported on HTTP.
	return c.sub.SubscribeFilterLogs(reqCtx, q, ch)
}

func (c *ethClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.queryOrSub().FilterLogs(reqCtx, q)
}

func (c *ethClient) SubscribeNewHead(ctx context.Context, heads chan *ethtypes.Header) (ethereum.Subscription, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	// Always uses the subscription transport — eth_subscribe is unsupported on HTTP.
	return c.sub.SubscribeNewHead(reqCtx, heads)
}

func (c *ethClient) ChainID(ctx context.Context) (*big.Int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.queryOrSub().ChainID(reqCtx)
}

func (c *ethClient) Close() {
	c.sub.Close()
	if c.query != nil {
		c.query.Close()
	}
}
