package controller

import (
	"sync"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

type Entity interface {
	RecipientID() string
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
	s.entities[agent.RecipientID()] = agent
	s.mu.Unlock()
}

func (s *entityService) Has(recipientID string) bool {
	s.mu.RLock()
	_, ok := s.entities[recipientID]
	s.mu.RUnlock()
	return ok
}

func (s *entityService) PushMessage(msg *queue.DecodedSSVMessage) {
	s.mu.RLock()
	agent := s.entities[msg.MsgID.GetRecipientID()]
	s.mu.RUnlock()

	if agent != nil {
		agent.PushMessage(msg)
	}
}

func (s *entityService) Kill(recipientID string) {
	s.mu.Lock()
	agent := s.entities[recipientID]
	s.mu.Unlock()

	if agent != nil {
		agent.Stop()
		delete(s.entities, recipientID)
	}
}
