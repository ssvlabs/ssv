package sync

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/ibft/sync/v1/handlers"
	"github.com/bloxapp/ssv/ibft/sync/v1/history"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	p2p "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	testingprotocol "github.com/bloxapp/ssv/protocol/v1/testing"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestHistory_SyncDecided(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nNodes := 4
	loggerFactory := func(who string) *zap.Logger {
		logger := zaptest.NewLogger(t).With(zap.String("who", who))
		//logger := zap.L().With(zap.String("who", who))
		return logger
	}
	ln, err := p2p.CreateAndStartLocalNet(ctx, loggerFactory, forksprotocol.V1ForkVersion, nNodes, nNodes/2, false)
	require.NoError(t, err)

	oids := make([]message.OperatorID, 0)
	for i := 1; i <= len(ln.NodeKeys); i++ {
		oids = append(oids, message.OperatorID(i))
	}

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"a4fc8c859ed5c10d7a1ff9fb111b76df3f2e0a6cbe7d0c58d3c98973c0ff160978bc9754a964b24929fff486ebccb629"}
	nShares := len(pks)

	type validatorData struct {
		PK      string
		sks     map[message.OperatorID]*bls.SecretKey
		nodes   map[message.OperatorID]*message.Node
		decided []*message.SignedMessage
	}
	validators := make([]*validatorData, 0)

	for i := 0; i < nShares; i++ {
		sks, nodes := testingprotocol.GenerateBLSKeys(oids...)
		pk, err := hex.DecodeString(pks[i])
		require.NoError(t, err)
		decided, err := testingprotocol.CreateMultipleDecidedMessages(sks, message.Height(0), message.Height(10),
			func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
				return oids[1:], &message.ConsensusMessage{
					MsgType:    message.CommitMsgType,
					Height:     height,
					Round:      1,
					Identifier: message.NewIdentifier(pk, beacon.RoleTypeAttester),
					Data:       []byte("data"),
				}
			})
		require.NoError(t, err)
		validators = append(validators, &validatorData{
			PK:      pks[i],
			sks:     sks,
			nodes:   nodes,
			decided: decided,
		})
	}

	stores := make([]qbftstorage.QBFTStore, 0)
	histories := make([]history.History, 0)
	for i, node := range ln.Nodes {
		store, err := newTestIbftStorage(loggerFactory(fmt.Sprintf("ibft-store-%d", i)), "test")
		require.NoError(t, err)
		stores = append(stores, store)
		valPipeline := validation.WrapFunc(fmt.Sprintf("history_sync-validation-%d", i), func(signedMessage *message.SignedMessage) error {
			return nil
		})
		h := history.New(loggerFactory(fmt.Sprintf("history_sync-%d", i)), store, node, valPipeline)
		histories = append(histories, h)
		f := forksv1.New()
		pid, _ := f.DecidedHistoryProtocol()
		node.RegisterHandler(string(pid),
			handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-%d", i)), store, 10))
		pid, _ = f.LastDecidedProtocol()
		node.RegisterHandler(string(pid),
			handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("last-decided-%d", i)), store))
		//pid, _ = f.LastChangeRoundProtocol()
		//node.RegisterHandler(string(pid),
		//	handlers.LastChangeRoundHandler(loggerFactory(fmt.Sprintf("change-round-%d", i)), store))
	}
	require.Len(t, histories, len(ln.Nodes))

	require.NoError(t, stores[0].SaveDecided(validators[0].decided[:5]...))
	require.NoError(t, stores[0].SaveLastDecided(validators[0].decided[5]))
	require.NoError(t, stores[3].SaveDecided(validators[0].decided...))
	require.NoError(t, stores[3].SaveLastDecided(validators[0].decided[9]))
	require.NoError(t, stores[3].SaveDecided(validators[1].decided...))
	require.NoError(t, stores[3].SaveLastDecided(validators[1].decided[9]))
	require.NoError(t, stores[1].SaveDecided(validators[0].decided...))
	require.NoError(t, stores[1].SaveLastDecided(validators[0].decided[9]))
	require.NoError(t, stores[1].SaveDecided(validators[1].decided...))
	require.NoError(t, stores[1].SaveLastDecided(validators[1].decided[9]))
	require.NoError(t, stores[2].SaveDecided(validators[0].decided...))
	require.NoError(t, stores[2].SaveLastDecided(validators[0].decided[9]))

	for _, pkHex := range pks {
		pk, err := hex.DecodeString(pkHex)
		require.NoError(t, err)
		for _, node := range ln.Nodes {
			require.NoError(t, node.Subscribe(pk))
		}
	}

	<-time.After(time.Second * 3)

	// performs sync from the first node
	for _, pkHex := range pks {
		pk, err := hex.DecodeString(pkHex)
		require.NoError(t, err)
		idn := message.NewIdentifier(pk, beacon.RoleTypeAttester)
		synced, err := histories[0].SyncDecided(ctx, idn, false)
		require.NoError(t, err)
		require.True(t, synced)
	}
}

func newTestIbftStorage(logger *zap.Logger, prefix string) (qbftstorage.QBFTStore, error) {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger.With(zap.String("who", "badger")),
		Path:   "",
	})
	if err != nil {
		return nil, err
	}
	return storage.New(db, logger.With(zap.String("who", "ibftStorage")), prefix), nil
}
