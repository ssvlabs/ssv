package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"sort"
)

// By is function to compare messages
type By func(a, b *message.SSVMessage) bool

// Combine runs current By and if result is negative, tries to run the other By.
func (by By) Combine(other By) By {
	return func(a, b *message.SSVMessage) bool {
		if !by(a, b) {
			return other(a, b)
		}
		return true
	}
}

// Sort sorts the given containers
func (by By) Sort(msgs []*msgContainer) {
	sort.Sort(&messageSorter{
		msgs: msgs,
		by:   by,
	})
}

// Add adds a new container
func (by By) Add(msgs []*msgContainer, msg *msgContainer) []*msgContainer {
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
func ByRound() By {
	return func(a, b *message.SSVMessage) bool {
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
}

// ByConsensusMsgType implements By for msg type based priority ()
func ByConsensusMsgType(messageTypes ...message.ConsensusMessageType) By {
	// using a single map to lookup msg types order
	m := map[message.ConsensusMessageType]int{}
	for i, mt := range messageTypes {
		m[mt] = i + 1
	}
	return func(a, b *message.SSVMessage) bool {
		aMsgType, ok := getConsensusMsgType(a)
		if !ok {
			return false
		}
		bMsgType, ok := getConsensusMsgType(b)
		if !ok {
			return true
		}

		// check according to predefined set
		aVal, aOk := m[aMsgType]
		bVal, bOk := m[bMsgType]
		if !aOk {
			return false
		}
		if !bOk {
			return true
		}
		return aVal > bVal
	}
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
