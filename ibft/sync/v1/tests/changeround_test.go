package tests

//
//import (
//	"context"
//	"encoding/hex"
//	"fmt"
//	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
//	"sync/atomic"
//	"testing"
//	"time"
//
//	"github.com/bloxapp/ssv/ibft/sync/v1/changeround"
//	"github.com/bloxapp/ssv/ibft/sync/v1/handlers"
//	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
//	"github.com/bloxapp/ssv/protocol/v1/message"
//	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
//	"github.com/stretchr/testify/require"
//	"go.uber.org/zap"
//	"go.uber.org/zap/zaptest"
//)
//
//func Test_LastChangeRound(t *testing.T) {
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	nNodes := 4
//	loggerFactory := func(who string) *zap.Logger {
//		logger := zaptest.NewLogger(t).With(zap.String("who", who))
//		// logger := zap.L().With(zap.String("who", who))
//		return logger
//	}
//	round := uint64(1)
//	pks := []string{
//		"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
//		"a4fc8c859ed5c10d7a1ff9fb111b76df3f2e0a6cbe7d0c58d3c98973c0ff160978bc9754a964b24929fff486ebccb629",
//	}
//	ln, validators, err := createNetworkWithValidators(ctx, loggerFactory, nNodes, pks, changeRoundGenerator(func() message.Round {
//		return message.Round(atomic.LoadUint64(&round))
//	}))
//	require.NoError(t, err)
//
//	stores := make([]qbftstorage.QBFTStore, 0)
//	lastRoundFetchers := make([]changeround.Fetcher, 0)
//	for i, node := range ln.Nodes {
//		store, err := newTestIbftStorage(loggerFactory(fmt.Sprintf("ibft-store-%d", i)), "test")
//		require.NoError(t, err)
//		stores = append(stores, store)
//		valPipeline := pipelines.WrapFunc(fmt.Sprintf("changeround-validation-%d", i), func(signedMessage *message.SignedMessage) error {
//			return nil
//		})
//		lastRoundFetcher := changeround.NewLastRoundFetcher(loggerFactory(fmt.Sprintf("fetcher-%d", i)), node, valPipeline)
//		lastRoundFetchers = append(lastRoundFetchers, lastRoundFetcher)
//		f := forksv1.New()
//		pid, _ := f.LastChangeRoundProtocol()
//		node.RegisterHandler(string(pid),
//			handlers.LastChangeRoundHandler(loggerFactory(fmt.Sprintf("change-round-%d", i)), store, node))
//	}
//	require.Len(t, lastRoundFetchers, len(ln.Nodes))
//
//	for _, pkHex := range pks {
//		pk, err := hex.DecodeString(pkHex)
//		require.NoError(t, err)
//		for _, node := range ln.Nodes {
//			require.NoError(t, node.Subscribe(pk))
//		}
//	}
//
//	<-time.After(time.Second * 3)
//
//	identifier1 := message.Identifier(validators[0].messages[0].Message.Identifier)
//	require.NoError(t, stores[2].SaveLastChangeRoundMsg(identifier1, validators[0].messages[1]))
//	require.NoError(t, stores[3].SaveLastChangeRoundMsg(identifier1, validators[0].messages[1]))
//
//	results, err := lastRoundFetchers[1].GetChangeRoundMessages(identifier1, validators[0].messages[1].Message.Height)
//	require.NoError(t, err)
//	require.Len(t, results, 2)
//}
