package protcolp2p

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

// MockMessageEvent is an abstraction used to push stream/pubsub messages
type MockMessageEvent struct {
	From     peer.ID
	Topic    string
	Protocol string
	Msg      *message.SSVMessage
}

// MockNetwork is a wrapping interface that enables tests to run with local network
type MockNetwork interface {
	Network

	SendStreamMessage(protocol string, pi peer.ID, msg *message.SSVMessage) error
	Self() peer.ID
	PushMsg(e MockMessageEvent)
	AddPeers(pk message.ValidatorPK, toAdd ...MockNetwork)
	Start(ctx context.Context)
	SetLastDecidedHandler(lastDecidedHandler EventHandler)
	SetGetHistoryHandler(getHistoryHandler EventHandler)
}

// EventHandler represents a function that handles a message event
type EventHandler func(e MockMessageEvent) *message.SSVMessage

// TODO: cleanup mockNetwork
type mockNetwork struct {
	logger *zap.Logger
	self   peer.ID

	topicsLock sync.Locker
	topics     map[string][]peer.ID

	subscribedLock sync.Locker
	subscribed     map[string]bool

	handlersLock sync.Locker
	handlers     map[string]RequestHandler

	inBufSize int
	inPubsub  chan MockMessageEvent
	inStream  chan MockMessageEvent

	peersLock sync.Locker
	peers     map[peer.ID]MockNetwork

	messagesLock sync.Locker
	messages     map[string]*message.SSVMessage

	lastDecidedHandler EventHandler
	getHistoryHandler  EventHandler

	lastDecidedResultsLock sync.Locker
	lastDecidedResults     []SyncResult

	getHistoryResultsLock sync.Locker
	getHistoryResults     []SyncResult

	lastDecidedReady chan struct{}
	getHistoryReady  chan struct{}
}

// NewMockNetwork creates a new instance of MockNetwork
func NewMockNetwork(logger *zap.Logger, self peer.ID, inBufSize int) MockNetwork {
	return &mockNetwork{
		logger:                 logger,
		self:                   self,
		topics:                 make(map[string][]peer.ID),
		peers:                  make(map[peer.ID]MockNetwork),
		inBufSize:              inBufSize,
		inPubsub:               make(chan MockMessageEvent, inBufSize),
		inStream:               make(chan MockMessageEvent, inBufSize),
		messages:               make(map[string]*message.SSVMessage),
		lastDecidedReady:       make(chan struct{}),
		getHistoryReady:        make(chan struct{}),
		topicsLock:             &sync.Mutex{},
		subscribedLock:         &sync.Mutex{},
		handlersLock:           &sync.Mutex{},
		peersLock:              &sync.Mutex{},
		messagesLock:           &sync.Mutex{},
		lastDecidedResultsLock: &sync.Mutex{},
		getHistoryResultsLock:  &sync.Mutex{},
	}
}

func (m *mockNetwork) SetLastDecidedHandler(lastDecidedHandler EventHandler) {
	m.lastDecidedHandler = lastDecidedHandler
}

func (m *mockNetwork) SetGetHistoryHandler(getHistoryHandler EventHandler) {
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
					m.lastDecidedResults = append(m.lastDecidedResults, SyncResult{
						Msg:    m.lastDecidedHandler(e),
						Sender: e.From.String(),
					})
					m.lastDecidedResultsLock.Unlock()
					close(m.lastDecidedReady)
				}
				if m.getHistoryHandler != nil {
					m.getHistoryResultsLock.Lock()
					m.getHistoryResults = append(m.getHistoryResults, SyncResult{
						Msg:    m.getHistoryHandler(e),
						Sender: e.From.String(),
					})
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

func (m *mockNetwork) Subscribe(pk message.ValidatorPK) error {
	spk := hex.EncodeToString(pk)

	m.subscribedLock.Lock()
	defer m.subscribedLock.Unlock()
	m.subscribed[spk] = true
	return nil
}

func (m *mockNetwork) Unsubscribe(pk message.ValidatorPK) error {
	m.subscribedLock.Lock()
	defer m.subscribedLock.Unlock()

	spk := hex.EncodeToString(pk)
	delete(m.subscribed, spk)

	return nil
}

func (m *mockNetwork) Peers(pk message.ValidatorPK) ([]peer.ID, error) {
	spk := hex.EncodeToString(pk)

	m.topicsLock.Lock()
	peers, ok := m.topics[spk]
	m.topicsLock.Unlock()

	if !ok {
		return nil, nil
	}
	return peers, nil
}

func (m *mockNetwork) Broadcast(msg message.SSVMessage) error {
	pk := msg.GetIdentifier().GetValidatorPK()
	spk := hex.EncodeToString(pk)
	topic := spk

	e := MockMessageEvent{
		From:  m.self,
		Topic: topic,
		Msg:   &msg,
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

	return nil
}

func (m *mockNetwork) RegisterHandlers(handlers ...*SyncHandler) {
	all := make(map[SyncProtocol][]RequestHandler)
	for _, h := range handlers {
		rhandlers, ok := all[h.Protocol]
		if !ok {
			rhandlers = make([]RequestHandler, 0)
		}
		rhandlers = append(rhandlers, h.Handler)
		all[h.Protocol] = rhandlers
	}

	for p, hs := range all {
		m.registerHandler(p, hs...)
	}
}

func (m *mockNetwork) registerHandler(protocol SyncProtocol, handlers ...RequestHandler) {
	var pid string
	switch protocol {
	case LastDecidedProtocol:
		pid = "/decided/last/0.0.1"
	case LastChangeRoundProtocol:
		pid = "/changeround/last/0.0.1"
	case DecidedHistoryProtocol:
		pid = "/decided/history/0.0.1"
	}

	requestHandlers := CombineRequestHandlers(handlers...)

	m.handlersLock.Lock()
	defer m.handlersLock.Unlock()
	m.handlers[pid] = requestHandlers
}

func (m *mockNetwork) LastDecided(mid message.Identifier) ([]SyncResult, error) {
	//m.lock.Lock()
	//defer m.lock.Unlock()

	spk := hex.EncodeToString(mid.GetValidatorPK())
	topic := spk

	syncMsg, err := (&message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
		Protocol: message.LastDecidedType,
		Status:   message.StatusSuccess,
	}).Encode()
	if err != nil {
		return nil, err
	}

	msg := &message.SSVMessage{
		Data:    syncMsg,
		ID:      mid,
		MsgType: message.SSVSyncMsgType,
	}

	m.topicsLock.Lock()
	ids := m.topics[topic]
	m.topicsLock.Unlock()

	for _, pi := range ids {
		if err := m.SendStreamMessage("last_decided", pi, msg); err != nil {
			return nil, err
		}
	}

	return m.PollLastDecidedMessages(), nil
}

func (m *mockNetwork) GetHistory(mid message.Identifier, from, to message.Height, targets ...string) ([]SyncResult, error) {
	// TODO: remove hardcoded return, use input parameters
	return m.PollGetHistoryMessages(), nil
}

func (m *mockNetwork) LastChangeRound(mid message.Identifier, height message.Height) ([]SyncResult, error) {
	//m.lock.Lock()
	//defer m.lock.Unlock()

	spk := hex.EncodeToString(mid.GetValidatorPK())
	topic := spk

	syncMsg, err := json.Marshal(&message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
	})
	if err != nil {
		return nil, err
	}

	msg := &message.SSVMessage{
		Data:    syncMsg,
		ID:      mid,
		MsgType: message.SSVSyncMsgType,
	}

	m.topicsLock.Lock()
	ids := m.topics[topic]
	m.topicsLock.Unlock()

	for _, pi := range ids {
		if err := m.SendStreamMessage("last_changeround", pi, msg); err != nil {
			return nil, err
		}
	}

	return nil, nil // TODO: fix returned value
}

func (m *mockNetwork) ReportValidation(message *message.SSVMessage, res MsgValidationResult) {
	panic("implement me")
}

func (m *mockNetwork) SendStreamMessage(protocol string, pi peer.ID, msg *message.SSVMessage) error {
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

func (m *mockNetwork) PollLastDecidedMessages() []SyncResult {
	<-m.lastDecidedReady

	m.lastDecidedResultsLock.Lock()
	defer m.lastDecidedResultsLock.Unlock()

	return m.lastDecidedResults
}

func (m *mockNetwork) PollGetHistoryMessages() []SyncResult {
	<-m.getHistoryReady

	m.getHistoryResultsLock.Lock()
	defer m.getHistoryResultsLock.Unlock()

	return m.getHistoryResults
}

// AddPeers enables to inject other peers
func (m *mockNetwork) AddPeers(pk message.ValidatorPK, toAdd ...MockNetwork) {
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

// GenPeerID generates a new network key
func GenPeerID() (peer.ID, error) {
	privKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return "", errors.WithMessage(err, "failed to generate 256k1 key")
	}
	return peer.IDFromPrivateKey(privKey)
}
