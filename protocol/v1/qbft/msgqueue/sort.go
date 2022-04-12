package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"sort"
)

// By is function to compare messages in clean way
type By func(a, b *message.SSVMessage) bool

// Sort sorts the given containers
func Sort(msgs []*msgContainer, by By) {
	sort.Sort(&messageSorter{
		msgs: msgs,
		by:   by,
	})
}

// Add adds a new container
func Add(msgs []*msgContainer, msg *msgContainer, by By) []*msgContainer {
	i := sort.Search(len(msgs), func(i int) bool { return by(msgs[i].msg, msg.msg) })
	var newMsgs []*msgContainer
	if i > 0 {
		newMsgs = append(msgs[:i], msg)
		newMsgs = append(newMsgs, msgs[i:]...)
		return newMsgs
	}
	newMsgs = append(append(newMsgs, msg), msgs...)
	return newMsgs
}

// ByRound implements By for round based priority
func ByRound(a, b *message.SSVMessage) bool {
	aRound, ok := getRound(a)
	if !ok {
		return false
	}
	bRound, ok := getRound(b)
	if !ok {
		return true
	}
	return aRound > bRound
}

// getRound returns the round of the message if applicable
func getRound(msg *message.SSVMessage) (message.Round, bool) {
	sm := message.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return 0, false
	}
	if sm.Message == nil {
		return 0, false
	}
	return sm.Message.Round, true
}

// messageSorter sorts a list of msg containers
type messageSorter struct {
	msgs []*msgContainer
	by   By
}

// Len is part of sort.Interface.
func (s *messageSorter) Len() int {
	return len(s.msgs)
}

// Swap is part of sort.Interface.
func (s *messageSorter) Swap(i, j int) {
	s.msgs[i], s.msgs[j] = s.msgs[j], s.msgs[i]
}

// Less is part of sort.Interface
func (s *messageSorter) Less(i, j int) bool {
	return s.by(s.msgs[i].msg, s.msgs[j].msg)
}
