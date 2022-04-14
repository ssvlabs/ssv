package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/sync/v1/handlers"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/sync/history"
)

func TestHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nNodes := 4
	loggerFactory := func(who string) *zap.Logger {
		logger := zaptest.NewLogger(t).With(zap.String("who", who))
		//logger := zap.L().With(zap.String("who", who))
		return logger
	}
	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"a4fc8c859ed5c10d7a1ff9fb111b76df3f2e0a6cbe7d0c58d3c98973c0ff160978bc9754a964b24929fff486ebccb629"}
	ln, validators, err := createNetworkWithValidators(ctx, loggerFactory, nNodes, pks, decidedGenerator)
	require.NoError(t, err)

	stores := make([]qbftstorage.QBFTStore, 0)
	histories := make([]history.History, 0)
	for i, node := range ln.Nodes {
		store, err := newTestIbftStorage(loggerFactory(fmt.Sprintf("ibft-store-%d", i)), "test")
		require.NoError(t, err)
		stores = append(stores, store)
		h := history.New(loggerFactory(fmt.Sprintf("history_sync-%d", i)), node)
		histories = append(histories, h)
		f := forksv1.New()
		pid, _ := f.DecidedHistoryProtocol()
		node.RegisterHandler(string(pid),
			handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-%d", i)), store, node, 10))
		pid, _ = f.LastDecidedProtocol()
		node.RegisterHandler(string(pid),
			handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("last-messages-%d", i)), store, node))
		//pid, _ = f.LastChangeRoundProtocol()
		//node.RegisterHandler(string(pid),
		//	handlers.LastChangeRoundHandler(loggerFactory(fmt.Sprintf("change-round-%d", i)), store))
	}
	require.Len(t, histories, len(ln.Nodes))

	msgSet1 := [][]*message.SignedMessage{
		validators[0].messages[:5],
		{validators[0].messages[5]},
	}

	msgSet2 := [][]*message.SignedMessage{
		append(validators[0].messages, validators[1].messages...),
		{validators[0].messages[9], validators[1].messages[9]},
	}

	msgSet3 := [][]*message.SignedMessage{
		validators[0].messages,
		{validators[0].messages[9]},
	}

	msgSet4 := msgSet2

	for storeIdx, msgSet := range [][][]*message.SignedMessage{msgSet1, msgSet2, msgSet3, msgSet4} {
		require.NoError(t, stores[storeIdx].SaveDecided(msgSet[0]...))
		require.NoError(t, stores[storeIdx].SaveDecided(msgSet[1]...))
	}

	for _, pkHex := range pks {
		pk, err := hex.DecodeString(pkHex)
		require.NoError(t, err)
		for _, node := range ln.Nodes {
			require.NoError(t, node.Subscribe(pk))
		}
	}

	<-time.After(time.Second * 3)

	t.Run("SyncDecided", func(t *testing.T) {
		// performs sync from the first node
		//for _, pkHex := range pks {
		//	pk, err := hex.DecodeString(pkHex)
		//	require.NoError(t, err)
		//	//idn := message.NewIdentifier(pk, message.RoleTypeAttester)
		//	//synced, err := histories[0].SyncDecided(ctx, idn, false)
		//	require.NoError(t, err)
		//	//require.True(t, synced)
		//}
	})

	t.Run("SyncDecidedInRange", func(t *testing.T) {
		//for _, pkHex := range pks {
		//	pk, err := hex.DecodeString(pkHex)
		//	require.NoError(t, err)
		//	idn := message.NewIdentifier(pk, message.RoleTypeAttester)
		//	synced, err := histories[1].SyncDecidedRange(ctx, idn, 0, 10)
		//	require.NoError(t, err)
		//	require.True(t, synced)
		//}
	})
}
