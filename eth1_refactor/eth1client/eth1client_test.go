package eth1client

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/networkconfig"
)

func TestEth1Client(t *testing.T) {
	const testTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	l, shutdownServer, err := setupServer(ctx)
	require.NoError(t, err)

	addr := "ws://" + l.Addr().String()
	contractAddr := networkconfig.TestNetwork.RegistryContractAddr
	logger := zaptest.NewLogger(t)

	client := New(addr, contractAddr, WithLogger(logger))

	require.NoError(t, client.Connect(ctx))

	ready, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, ready)

	require.NoError(t, client.Close())
	require.NoError(t, shutdownServer())
}

type testService struct{}

func (s *testService) Syncing(_ *struct{}, reply *bool) error {
	if reply != nil {
		*reply = false
	}

	return nil
}

func setupServer(ctx context.Context) (listener net.Listener, shutdown func() error, err error) {
	l, err := net.Listen("tcp", "")
	if err != nil {
		return nil, nil, err
	}

	rpcServer := ethrpc.NewServer()
	if err := rpcServer.RegisterName("eth", new(testService)); err != nil {
		return nil, nil, err
	}

	httpServer := &http.Server{
		Handler: rpcServer.WebsocketHandler([]string{"*"}),
	}

	serveErr := make(chan error)
	go func() {
		serveErr <- httpServer.Serve(l)
	}()

	shutdown = func() error {
		if err := httpServer.Shutdown(ctx); err != nil {
			return err
		}

		rpcServer.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-serveErr:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}

			return err
		}
	}

	return l, shutdown, nil
}
