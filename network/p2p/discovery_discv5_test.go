package p2p

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestIsRelevantNode(t *testing.T) {
	ctx := context.Background()

	relevant := make(map[string]bool)
	lookupHandler := func(oid string) bool {
		return relevant[oid]
	}

	n := &p2pNetwork{
		ctx:            ctx,
		logger:         zaptest.NewLogger(t),
		lookupOperator: lookupHandler,
		cfg:            &Config{NetworkTrace: true},
	}

	t.Run("relevant operator", func(t *testing.T) {
		node, _ := createPeer(t, Operator)
		oid, err := extractOperatorIDEntry(node.Record())
		require.NoError(t, err)
		relevant[string(*oid)] = true
		require.True(t, n.isRelevantNode(node))
	})

	t.Run("exporter node", func(t *testing.T) {
		node, _ := createPeer(t, Exporter)
		require.True(t, n.isRelevantNode(node))
	})

	t.Run("irrelevant node", func(t *testing.T) {
		node, _ := createPeer(t, Operator)
		require.False(t, n.isRelevantNode(node))
	})

	t.Run("unknown node", func(t *testing.T) {
		node, _ := createPeer(t, Unknown)
		// TODO: change in future, once supported
		require.True(t, n.isRelevantNode(node))
	})

	// TODO: unmark once supported
	//t.Run("bootnode", func(t *testing.T) {
	//	node, _ := createPeer(t, Bootnode)
	//	require.True(t, n.isRelevantNode(node))
	//})
}
