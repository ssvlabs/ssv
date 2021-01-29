package ibft

import "github.com/bloxapp/ssv/ibft/proto"

type State struct {
	Stage          proto.RoundState
	Lambda         []byte
	PreviousLambda []byte
	InputValue     []byte
	Round          uint64
	PreparedRound  uint64
	PreparedValue  []byte
}

func (s *State) PreviouslyPrepared() bool {
	return s.PreparedRound != 0 && s.PreparedValue != nil
}
