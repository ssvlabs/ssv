package types

type State struct {
	Stage         RoundState
	Lambda        []byte
	InputValue    []byte
	Round         uint64
	PreparedRound uint64
	PreparedValue []byte
}

func (s *State) PreviouslyPrepared() bool {
	return s.PreparedRound != 0 && s.PreparedValue != nil
}
