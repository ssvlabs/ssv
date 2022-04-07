package history

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"sync"
)

type dummyDecidedStore struct {
	tips map[string]*message.SignedMessage
	msgs map[string]map[message.Height]*message.SignedMessage
	lock *sync.RWMutex
}

var _ qbftstorage.DecidedMsgStore = &dummyDecidedStore{}

func newDecidedStoreMock() *dummyDecidedStore {
	return &dummyDecidedStore{}
}

func (dsm *dummyDecidedStore) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	dsm.lock.RLock()
	defer dsm.lock.RUnlock()

	msg, ok := dsm.tips[string(identifier)]
	if !ok && msg != nil {
		// TODO: return err not found
		return nil, nil
	}

	return msg, nil
}

func (dsm *dummyDecidedStore) SaveLastDecided(signedMsg ...*message.SignedMessage) error {
	dsm.lock.Lock()
	defer dsm.lock.Unlock()

	for _, sm := range signedMsg {
		dsm.tips[string(sm.Message.Identifier)] = sm
	}

	return nil
}

func (dsm *dummyDecidedStore) GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error) {
	dsm.lock.RLock()
	defer dsm.lock.RUnlock()

	messages, ok := dsm.msgs[string(identifier)]
	n := len(messages)
	if !ok || n == 0 {
		// TODO: return err not found
		return nil, nil
	}
	if message.Height(n) <= to {
		to = from + message.Height(n)
	}
	results := make([]*message.SignedMessage, 0)
	for i := from; i < message.Height(n); i++ {
		results = append(results, messages[i])
	}

	return results, nil
}

func (dsm *dummyDecidedStore) SaveDecided(signedMsg ...*message.SignedMessage) error {
	dsm.lock.Lock()
	defer dsm.lock.Unlock()

	for _, sm := range signedMsg {
		if sm == nil || sm.Message == nil {
			continue
		}
		messages, ok := dsm.msgs[string(sm.Message.Identifier)]
		if !ok {
			messages = make(map[message.Height]*message.SignedMessage, 0)
		}
		messages[sm.Message.Height] = sm
	}

	return nil
}

//type dummySyncer struct {
//	handlers map[string]p2pprotocol.RequestHandler
//	stores map[string]qbftstorage.DecidedMsgStore
//	lock *sync.RWMutex
//}
//
//var _ p2pprotocol.Syncer = &dummySyncer{}
//
//func newSyncerMock(stores map[string]qbftstorage.DecidedMsgStore) *dummySyncer {
//	return &dummySyncer{
//		handlers: map[string]p2pprotocol.RequestHandler{},
//		stores: stores,
//		lock: &sync.RWMutex{},
//	}
//}
//
//func (s dummySyncer) RegisterHandler(protocol string, handler p2pprotocol.RequestHandler) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//
//	s.handlers[protocol] = handler
//}
//
//func (s dummySyncer) LastDecided(mid message.Identifier) ([]p2pprotocol.SyncResult, error) {
//	res := make([]p2pprotocol.SyncResult, 0)
//	for sender, store := range s.stores {
//		decided, err := store.GetLastDecided(mid)
//		if err != nil {
//			return nil, err
//		}
//		data, err := decided.Encode()
//		if err != nil {
//			return nil, err
//		}
//		res = append(res, p2pprotocol.SyncResult{
//			Msg: &message.SSVMessage{
//				MsgType: message.SSVSyncMsgType,
//				ID:      decided.Message.Identifier,
//				Data:    data,
//			},
//			Sender: sender,
//		})
//	}
//	return res, nil
//}
//
//func (s dummySyncer) GetHistory(mid message.Identifier, from, to message.Height, targets ...string) ([]p2pprotocol.SyncResult, error) {
//	panic("implement me")
//}
//
//func (s dummySyncer) LastChangeRound(mid message.Identifier, height message.Height) ([]p2pprotocol.SyncResult, error) {
//	panic("implement me")
//}
//
//
