package protcolp2p

import (
	crand "crypto/rand"
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

type MockMessageEvent struct {
	From     peer.ID
	Topic    string
	Protocol string
	Msg      *message.SSVMessage
}

type MockNetwork interface {
	Network

	SendStreamMessage(protocol string, pi peer.ID, msg *message.SSVMessage) error
	Self() peer.ID
	PushMsg(e MockMessageEvent)
	AddPeers(pk message.ValidatorPK, toAdd ...MockNetwork)
}

type mockNetwork struct {
	logger *zap.Logger
	self   peer.ID

	lock sync.Locker

	topics     map[string][]peer.ID
	subscribed map[string]bool

	handlers map[string]RequestHandler

	inBufSize int
	inPubsub  chan MockMessageEvent
	inStream  chan MockMessageEvent

	peers map[peer.ID]MockNetwork
}

func NewMockNetwork(logger *zap.Logger, self peer.ID, inBufSize int) MockNetwork {
	return &mockNetwork{
		logger:    logger,
		self:      self,
		lock:      &sync.Mutex{},
		topics:    make(map[string][]peer.ID),
		peers:     make(map[peer.ID]MockNetwork),
		inBufSize: inBufSize,
		inPubsub:  make(chan MockMessageEvent, inBufSize),
		inStream:  make(chan MockMessageEvent, inBufSize),
	}
}

func (m *mockNetwork) Subscribe(pk message.ValidatorPK) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	m.subscribed[spk] = true
	return nil
}

func (m *mockNetwork) Unsubscribe(pk message.ValidatorPK) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	delete(m.subscribed, spk)

	return nil
}

func (m *mockNetwork) Peers(pk message.ValidatorPK) ([]peer.ID, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	peers, ok := m.topics[spk]
	if !ok {
		return nil, nil
	}
	return peers, nil
}

func (m *mockNetwork) Broadcast(msg message.SSVMessage) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	pk := msg.GetIdentifier().GetValidatorPK()
	spk := hex.EncodeToString(pk)
	topic := spk

	e := MockMessageEvent{
		From:  m.self,
		Topic: topic,
		Msg:   &msg,
	}

	for _, pi := range m.topics[topic] {
		mn, ok := m.peers[pi]
		if !ok {
			continue
		}
		mn.PushMsg(e)
	}

	return nil
}

func (m *mockNetwork) RegisterHandler(protocol SyncProtocol, handler RequestHandler) {
	m.lock.Lock()
	defer m.lock.Unlock()

	var pid string
	switch protocol {
	case LastDecidedProtocol:
		pid = "/decided/last/0.0.1"
	case LastChangeRoundProtocol:
		pid = "/changeround/last/0.0.1"
	case DecidedHistoryProtocol:
		pid = "/decided/history/0.0.1"
	}

	m.handlers[pid] = handler
}

func (m *mockNetwork) LastDecided(mid message.Identifier) ([]SyncResult, error) {
	panic("implement me")

	//m.lock.Lock()
	//defer m.lock.Unlock()
	//
	//spk := hex.EncodeToString(mid.GetValidatorPK())
	//topic := spk
	//
	//syncMsg, err := json.Marshal(&message.SyncMessage{
	//	Params: &message.SyncParams{
	//		Identifier: mid,
	//	},
	//})
	//if err != nil {
	//	return nil, err
	//}
	//
	//msg := &message.SSVMessage{
	//	Data: syncMsg,
	//	ID: mid,
	//	MsgType: message.SSVSyncMsgType,
	//}
	//
	//for _, pi := range m.topics[topic] {
	//	if err := m.SendStreamMessage("last_decided", pi, msg); err != nil {
	//		return nil, err
	//	}
	//}
}

func (m *mockNetwork) GetHistory(mid message.Identifier, from, to message.Height, targets ...string) ([]SyncResult, error) {
	panic("implement me")
}

func (m *mockNetwork) LastChangeRound(mid message.Identifier, height message.Height) ([]SyncResult, error) {
	panic("implement me")
}

func (m *mockNetwork) ReportValidation(message *message.SSVMessage, res MsgValidationResult) {
	panic("implement me")
}

func (m *mockNetwork) SendStreamMessage(protocol string, pi peer.ID, msg *message.SSVMessage) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	e := MockMessageEvent{
		From:     m.self,
		Protocol: protocol,
		Msg:      msg,
	}

	mn, ok := m.peers[pi]
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
	m.lock.Lock()
	defer m.lock.Unlock()

	if len(m.inPubsub) < m.inBufSize {
		m.inPubsub <- e
	}
}

// AddPeers enables to inject other peers
func (m *mockNetwork) AddPeers(pk message.ValidatorPK, toAdd ...MockNetwork) {
	m.lock.Lock()
	defer m.lock.Unlock()

	// TODO: support subnets
	spk := hex.EncodeToString(pk)

	peers, ok := m.topics[spk]
	if !ok {
		peers = make([]peer.ID, 0)
	}

	for _, node := range toAdd {
		pi := node.Self()
		if _, ok := m.peers[pi]; !ok {
			m.peers[pi] = node
		}
		peers = append(peers, pi)
	}
	m.topics[spk] = peers
}

// GenPeerID generates a new network key
func GenPeerID() (peer.ID, error) {
	privKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return "", errors.WithMessage(err, "failed to generate 256k1 key")
	}
	return peer.IDFromPrivateKey(privKey)
}
