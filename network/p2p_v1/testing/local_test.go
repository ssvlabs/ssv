package testing

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestNewLocalNet(t *testing.T) {
	n := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	t.Logf("creating %d keys", n)
	identities, err := CreateKeys(n)
	require.NoError(t, err)
	t.Logf("creating local mdns network with %d nodes", n)
	ln, err := NewLocalNet(ctx, logger, identities...)
	require.NoError(t, err)
	require.Len(t, ln.Nodes, n)
}

func TestNewLocalDiscV5Net(t *testing.T) {
	n := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	t.Logf("creating %d keys", n)
	identities, err := CreateKeys(n)
	require.NoError(t, err)
	t.Logf("creating local discv5 network with %d nodes and bootnode", n)
	ln, err := NewLocalDiscV5Net(ctx, logger, identities...)
	require.NoError(t, err)
	require.NotNil(t, ln.Bootnode)
	require.Len(t, ln.Nodes, n)
}
