package controller

import (
	"sync"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

type Entity interface {
	SenderID() []byte
	PushMessage(msg *queue.DecodedSSVMessage)
	Stop()
}

type entityService struct {
	entities map[string]Entity
	mu       sync.RWMutex
}

func newEntityService() *entityService {
	return &entityService{
		entities: make(map[string]Entity),
	}
}

func (s *entityService) Register(agent Entity) {
	s.mu.Lock()
	s.entities[string(agent.SenderID())] = agent
	s.mu.Unlock()
}

func (s *entityService) Has(senderID []byte) bool {
	s.mu.RLock()
	_, ok := s.entities[string(senderID)]
	s.mu.RUnlock()
	return ok
}

func (s *entityService) PushMessage(msg *queue.DecodedSSVMessage) {
	s.mu.RLock()
	agent := s.entities[string(msg.MsgID.GetSenderID())]
	s.mu.RUnlock()

	if agent != nil {
		agent.PushMessage(msg)
	}
}

func (s *entityService) Kill(senderID string) {
	s.mu.Lock()
	agent := s.entities[senderID]
	s.mu.Unlock()

	if agent != nil {
		agent.Stop()
		delete(s.entities, senderID)
	}
}
