package testing

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/ssvlabs/ssv/protocol/genesis/message"
	protocolp2p "github.com/ssvlabs/ssv/protocol/genesis/p2p"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"
)

// MockMessageEvent is an abstraction used to push stream/pubsub messages
type MockMessageEvent struct {
	From     peer.ID
	Topic    string
	Protocol string
	Msg      *genesisspectypes.SSVMessage
}

// testNetworkHelpers consist of helper functions for tests
type testNetworkHelpers interface {
	SendStreamMessage(protocol string, pi peer.ID, msg *genesisspectypes.SSVMessage) error
	Self() peer.ID
	PushMsg(e MockMessageEvent)
	AddPeers(pk genesisspectypes.ValidatorPK, toAdd ...TestNetwork)
	Start(ctx context.Context)
	SetLastDecidedHandler(lastDecidedHandler TestEventHandler)
	SetGetHistoryHandler(getHistoryHandler TestEventHandler)
}

// TestNetwork is a wrapping interface that enables tests to run with local network
type TestNetwork interface {
	protocolp2p.Network
	testNetworkHelpers
}

// TestEventHandler represents a function that handles a message event
type TestEventHandler func(e MockMessageEvent) genesisspectypes.SSVMessage

// TODO: cleanup mockNetwork
type mockNetwork struct {
	logger *zap.Logger
	self   peer.ID

	topicsLock sync.Locker
	topics     map[string][]peer.ID

	subscribedLock sync.Locker
	subscribed     map[string]bool

	handlersLock sync.Locker
	//handlers     map[string]RequestHandler

	inBufSize int
	inPubsub  chan MockMessageEvent
	inStream  chan MockMessageEvent

	peersLock sync.Locker
	peers     map[peer.ID]TestNetwork

	messagesLock sync.Locker
	messages     map[string]*genesisspectypes.SSVMessage

	broadcastMessagesLock sync.Locker
	broadcastMessages     []genesisspectypes.SSVMessage

	lastDecidedHandler TestEventHandler
	getHistoryHandler  TestEventHandler

	lastDecidedResultsLock sync.Locker
	lastDecidedResults     []genesisspectypes.SSVMessage

	getHistoryResultsLock sync.Locker
	getHistoryResults     []genesisspectypes.SSVMessage

	lastDecidedReady chan struct{}
	getHistoryReady  chan struct{}

	calledDecidedSyncCnt int
}

// NewMockNetwork creates a new instance of TestNetwork
func NewMockNetwork(logger *zap.Logger, self peer.ID, inBufSize int) TestNetwork {
	return &mockNetwork{
		logger:                 logger,
		self:                   self,
		topics:                 make(map[string][]peer.ID),
		peers:                  make(map[peer.ID]TestNetwork),
		inBufSize:              inBufSize,
		inPubsub:               make(chan MockMessageEvent, inBufSize),
		inStream:               make(chan MockMessageEvent, inBufSize),
		messages:               make(map[string]*genesisspectypes.SSVMessage),
		lastDecidedReady:       make(chan struct{}),
		getHistoryReady:        make(chan struct{}),
		topicsLock:             &sync.Mutex{},
		subscribedLock:         &sync.Mutex{},
		handlersLock:           &sync.Mutex{},
		peersLock:              &sync.Mutex{},
		messagesLock:           &sync.Mutex{},
		broadcastMessagesLock:  &sync.Mutex{},
		lastDecidedResultsLock: &sync.Mutex{},
		lastDecidedResults:     make([]genesisspectypes.SSVMessage, 0),
		getHistoryResultsLock:  &sync.Mutex{},
		getHistoryResults:      make([]genesisspectypes.SSVMessage, 0),
	}
}

func (m *mockNetwork) SetLastDecidedHandler(lastDecidedHandler TestEventHandler) {
	m.lastDecidedHandler = lastDecidedHandler
}

func (m *mockNetwork) SetGetHistoryHandler(getHistoryHandler TestEventHandler) {
	m.getHistoryHandler = getHistoryHandler
}

func (m *mockNetwork) Start(pctx context.Context) {
	go func() {
		ctx, cancel := context.WithCancel(pctx)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-m.inStream:
				if m.lastDecidedHandler != nil {
					m.lastDecidedResultsLock.Lock()

					m.lastDecidedResults = append(m.lastDecidedResults, m.lastDecidedHandler(e))
					m.lastDecidedResultsLock.Unlock()
					close(m.lastDecidedReady)
				}
				if m.getHistoryHandler != nil {
					m.getHistoryResultsLock.Lock()
					m.getHistoryResults = append(m.getHistoryResults, m.getHistoryHandler(e))
					m.getHistoryResultsLock.Unlock()
					close(m.getHistoryReady)
				}
			}
		}
	}()

	go func() {
		ctx, cancel := context.WithCancel(pctx)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-m.inPubsub:
				m.handleStreamEvent(e)
			}
		}
	}()
}

func (m *mockNetwork) handleStreamEvent(e MockMessageEvent) {
	m.messagesLock.Lock()
	defer m.messagesLock.Unlock()

	m.messages[e.Topic] = e.Msg
}

func (m *mockNetwork) Subscribe(pk genesisspectypes.ValidatorPK) error {
	spk := hex.EncodeToString(pk)

	m.subscribedLock.Lock()
	defer m.subscribedLock.Unlock()
	m.subscribed[spk] = true
	return nil
}

func (m *mockNetwork) Unsubscribe(logger *zap.Logger, pk genesisspectypes.ValidatorPK) error {
	m.subscribedLock.Lock()
	defer m.subscribedLock.Unlock()

	spk := hex.EncodeToString(pk)
	delete(m.subscribed, spk)

	return nil
}

func (m *mockNetwork) Peers(pk genesisspectypes.ValidatorPK) ([]peer.ID, error) {
	spk := hex.EncodeToString(pk)

	m.topicsLock.Lock()
	peers, ok := m.topics[spk]
	m.topicsLock.Unlock()

	if !ok {
		return nil, nil
	}
	return peers, nil
}

func (m *mockNetwork) Broadcast(msg *genesisspectypes.SSVMessage) error {
	pk := msg.GetID().GetPubKey()
	spk := hex.EncodeToString(pk)
	topic := spk

	e := MockMessageEvent{
		From:  m.self,
		Topic: topic,
		Msg:   msg,
	}

	m.topicsLock.Lock()
	ids := m.topics[topic]
	m.topicsLock.Unlock()

	for _, pi := range ids {
		m.peersLock.Lock()
		mn, ok := m.peers[pi]
		m.peersLock.Unlock()
		if !ok {
			continue
		}
		mn.PushMsg(e)
	}

	m.broadcastMessagesLock.Lock()
	m.broadcastMessages = append(m.broadcastMessages, *msg)
	m.broadcastMessagesLock.Unlock()

	return nil
}

func (m *mockNetwork) GetHistory(logger *zap.Logger, mid genesisspectypes.MessageID, from, to genesisspecqbft.Height, targets ...string) ([]protocolp2p.SyncResult, genesisspecqbft.Height, error) {
	return nil, 0, nil
}

func (m *mockNetwork) RegisterHandlers(logger *zap.Logger, handlers ...*protocolp2p.SyncHandler) {
	// TODO?
}

func (m *mockNetwork) LastDecided(logger *zap.Logger, mid genesisspectypes.MessageID) ([]protocolp2p.SyncResult, error) {
	return nil, nil
}

func (m *mockNetwork) ReportValidation(logger *zap.Logger, message *genesisspectypes.SSVMessage, res protocolp2p.MsgValidationResult) {
}

func (m *mockNetwork) SyncHighestDecided(mid genesisspectypes.MessageID) error {
	//m.lock.Lock()
	//defer m.lock.Unlock()

	m.logger.Debug("🔀 CALL SYNC")
	m.calledDecidedSyncCnt++

	spk := hex.EncodeToString(mid.GetPubKey())
	topic := spk

	syncMsg, err := (&message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
		Protocol: message.LastDecidedType,
		Status:   message.StatusSuccess,
	}).Encode()
	if err != nil {
		return err
	}

	msg := &genesisspectypes.SSVMessage{
		Data:    syncMsg,
		MsgID:   mid,
		MsgType: message.SSVSyncMsgType,
	}

	m.topicsLock.Lock()
	ids := m.topics[topic]
	m.topicsLock.Unlock()

	for _, pi := range ids {
		if err := m.SendStreamMessage("last_decided", pi, msg); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockNetwork) SyncDecidedByRange(identifier genesisspectypes.MessageID, to, from genesisspecqbft.Height) {
	//TODO implement me
}

func (m *mockNetwork) SyncHighestRoundChange(mid genesisspectypes.MessageID, height genesisspecqbft.Height) error {
	spk := hex.EncodeToString(mid.GetPubKey())
	topic := spk

	syncMsg, err := json.Marshal(&message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
	})
	if err != nil {
		return err
	}

	msg := &genesisspectypes.SSVMessage{
		Data:    syncMsg,
		MsgID:   mid,
		MsgType: message.SSVSyncMsgType,
	}

	m.topicsLock.Lock()
	ids := m.topics[topic]
	m.topicsLock.Unlock()

	for _, pi := range ids {
		if err := m.SendStreamMessage("last_changeround", pi, msg); err != nil {
			return err
		}
	}

	return nil
}

func (m *mockNetwork) SendStreamMessage(protocol string, pi peer.ID, msg *genesisspectypes.SSVMessage) error {
	e := MockMessageEvent{
		From:     m.self,
		Protocol: protocol,
		Msg:      msg,
	}

	m.peersLock.Lock()
	mn, ok := m.peers[pi]
	m.peersLock.Unlock()
	if !ok {
		return errors.New("peer not found")
	}
	mn.PushMsg(e)
	return nil
}

func (m *mockNetwork) Self() peer.ID {
	return m.self
}

func (m *mockNetwork) PushMsg(e MockMessageEvent) {
	if len(e.Topic) > 0 {
		if len(m.inPubsub) < m.inBufSize {
			m.inPubsub <- e
		}
	} else if len(e.Protocol) > 0 {
		if len(m.inStream) < m.inBufSize {
			m.inStream <- e
		}
	}
}

// AddPeers enables to inject other peers
func (m *mockNetwork) AddPeers(pk genesisspectypes.ValidatorPK, toAdd ...TestNetwork) {
	// TODO: support subnets
	spk := hex.EncodeToString(pk)

	m.topicsLock.Lock()
	peers, ok := m.topics[spk]
	m.topicsLock.Unlock()
	if !ok {
		peers = make([]peer.ID, 0)
	}

	for _, node := range toAdd {
		pi := node.Self()
		m.peersLock.Lock()
		if _, ok := m.peers[pi]; !ok {
			m.peers[pi] = node
		}
		m.peersLock.Unlock()
		peers = append(peers, pi)
	}

	m.topicsLock.Lock()
	m.topics[spk] = peers
	m.topicsLock.Unlock()
}

func (m *mockNetwork) GetBroadcastMessages() []genesisspectypes.SSVMessage {
	m.broadcastMessagesLock.Lock()
	defer m.broadcastMessagesLock.Unlock()

	return m.broadcastMessages
}

func (m *mockNetwork) CalledDecidedSyncCnt() int {
	return m.calledDecidedSyncCnt
}

func (m *mockNetwork) SetCalledDecidedSyncCnt(i int) {
	m.calledDecidedSyncCnt = i
}

// GenPeerID generates a new network key
func GenPeerID() (peer.ID, error) {
	privKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return "", errors.WithMessage(err, "failed to generate 256k1 key")
	}
	return peer.IDFromPrivateKey(privKey)
}
