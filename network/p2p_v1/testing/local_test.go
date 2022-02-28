package testing

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestNewLocalNet(t *testing.T) {
	n := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	identities, err := CreateKeys(n)
	require.NoError(t, err)
	ln, err := NewLocalNet(ctx, logger, identities...)
	require.NoError(t, err)
	require.Len(t, ln.Nodes, n)

	<-time.After(time.Second)
}

func TestNewLocalDiscV5Net(t *testing.T) {
	n := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	identities, err := CreateKeys(n)
	require.NoError(t, err)
	ln, err := NewLocalDiscV5Net(ctx, logger, identities...)
	require.NoError(t, err)
	require.NotNil(t, ln.Bootnode)
	require.Len(t, ln.Nodes, n)

	<-time.After(time.Second)
}
