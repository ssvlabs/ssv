package decided

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	dutytracer "github.com/ssvlabs/ssv/exporter/dutytracer"
	"github.com/ssvlabs/ssv/exporter/v1/api"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	mocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

// stubWebSocketServer is a minimal WebSocketServer implementation for testing:
// only BroadcastFeed is exercised by NewStreamPublisher/NewDecidedListener.
type stubWebSocketServer struct {
	feed *event.Feed
}

func newStubWebSocketServer() *stubWebSocketServer {
	return &stubWebSocketServer{feed: new(event.Feed)}
}

func (s *stubWebSocketServer) Start(_ context.Context) (string, <-chan error, error) {
	return "", nil, nil
}

func (s *stubWebSocketServer) BroadcastFeed() *event.Feed {
	return s.feed
}

func (s *stubWebSocketServer) UseQueryHandler(_ api.QueryMessageHandler) {}

func testNetCfg() *networkconfig.Network {
	return &networkconfig.Network{
		Beacon: networkconfig.TestNetwork.Beacon,
		SSV: &networkconfig.SSV{
			Name:           "test",
			DomainType:     spectypes.DomainType{1, 2, 3, 4},
			NextDomainType: spectypes.DomainType{5, 6, 7, 8},
			Forks:          networkconfig.SSVForks{Boole: 1000},
		},
	}
}

func TestNewStreamPublisher_BroadcastsAndDeduplicates(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ws := newStubWebSocketServer()
	netCfg := testNetCfg()

	handler := NewStreamPublisher(logger, netCfg, ws)

	received := make(chan api.Message, 4)
	sub := ws.feed.Subscribe(received)
	defer sub.Unsubscribe()

	pubKey := spectypes.ValidatorPK{1, 2, 3}
	msg := qbftstorage.Participation{
		ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
			PubKey:  pubKey,
			Slot:    10,
			Signers: []spectypes.OperatorID{1, 2, 3},
		},
		Role:   spectypes.BNRoleAttester,
		PubKey: pubKey,
	}

	handler(msg)
	require.Eventually(t, func() bool { return len(received) == 1 }, time.Second, 5*time.Millisecond)
	<-received // drain so the dedup check below observes only a potential second send

	// Same key (pubkey:slot:signer-count) within TTL must be deduplicated (no second send).
	handler(msg)
	select {
	case <-received:
		t.Fatal("duplicate message must not be broadcast again within cache TTL")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNewDecidedListener_SkipsUnknownValidatorIndex(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ws := newStubWebSocketServer()
	netCfg := testNetCfg()

	ctrl := gomock.NewController(t)
	validators := mocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().ValidatorPubkey(phase0.ValidatorIndex(42)).Return(spectypes.ValidatorPK{}, false)

	handler := NewDecidedListener(logger, netCfg, ws, validators)

	received := make(chan api.Message, 1)
	sub := ws.feed.Subscribe(received)
	defer sub.Unsubscribe()

	handler(dutytracer.DecidedInfo{
		Index:   42,
		Slot:    10,
		Role:    spectypes.BNRoleAttester,
		Signers: []spectypes.OperatorID{1},
	})

	select {
	case <-received:
		t.Fatal("must not broadcast when validator index cannot be resolved to a pubkey")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNewDecidedListener_BroadcastsAndDeduplicates(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ws := newStubWebSocketServer()
	netCfg := testNetCfg()

	ctrl := gomock.NewController(t)
	validators := mocks.NewMockValidatorStore(ctrl)
	pubKey := spectypes.ValidatorPK{7, 7, 7}
	validators.EXPECT().ValidatorPubkey(phase0.ValidatorIndex(7)).Return(pubKey, true).Times(2)

	handler := NewDecidedListener(logger, netCfg, ws, validators)

	received := make(chan api.Message, 4)
	sub := ws.feed.Subscribe(received)
	defer sub.Unsubscribe()

	info := dutytracer.DecidedInfo{
		Index:   7,
		Slot:    20,
		Role:    spectypes.BNRoleAttester,
		Signers: []spectypes.OperatorID{1, 2},
	}

	handler(info)
	require.Eventually(t, func() bool { return len(received) == 1 }, time.Second, 5*time.Millisecond)
	<-received // drain so the dedup check below observes only a potential second send

	// Same key (index:slot:signer-count) within TTL must be deduplicated.
	handler(info)
	select {
	case <-received:
		t.Fatal("duplicate message must not be broadcast again within cache TTL")
	case <-time.After(50 * time.Millisecond):
	}
}
