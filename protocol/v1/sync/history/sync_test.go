package history

//
//func TestHistory_SyncDecided(t *testing.T) {
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	logger := zap.L()
//
//	store := newDecidedStoreMock()
//
//
//	h := New(logger)
//}
//
//type decidedStoreMock struct {
//	msgs map[string]map[message.Height]*message.SignedMessage
//	lock *sync.RWMutex
//}
//
//var _ qbftstorage.DecidedMsgStore = &decidedStoreMock{}
//
//func newDecidedStoreMock() *decidedStoreMock {
//	return &decidedStoreMock{}
//}
//
//func (dsm *decidedStoreMock) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
//	dsm.lock.RLock()
//	defer dsm.lock.RUnlock()
//
//	messages, ok := dsm.msgs[string(identifier)]
//	if !ok || len(messages) == 0 {
//		// TODO: return err not found
//		return nil, nil
//	}
//
//	return messages[len(messages)-1], nil
//}
//
//func (dsm *decidedStoreMock) SaveLastDecided(signedMsg ...*message.SignedMessage) error {
//	dsm.lock.Lock()
//	defer dsm.lock.Unlock()
//
//	for _, sm := range signedMsg {
//		messages, ok := dsm.msgs[string(sm.Message.Identifier)]
//		if !ok {
//			messages = make([]*message.SignedMessage, 0)
//		}
//		messages = append(messages, sm)
//	}
//
//	return nil
//}
//
//func (dsm *decidedStoreMock) GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error) {
//	dsm.lock.RLock()
//	defer dsm.lock.RUnlock()
//
//	messages, ok := dsm.msgs[string(identifier)]
//	n := len(messages)
//	if !ok || n == 0 {
//		// TODO: return err not found
//		return nil, nil
//	}
//	if message.Height(n) <= to {
//		to = message.Height(n) - 1
//	}
//	return messages[from:to], nil
//}
//
//func (dsm *decidedStoreMock) SaveDecided(signedMsg ...*message.SignedMessage) error {
//	dsm.lock.Lock()
//	defer dsm.lock.Unlock()
//
//	for _, sm := range signedMsg {
//		messages, ok := dsm.msgs[string(sm.Message.Identifier)]
//		if !ok {
//			messages = make([]*message.SignedMessage, 0)
//		}
//		messages = append(messages, sm)
//	}
//
//	return nil
//}
