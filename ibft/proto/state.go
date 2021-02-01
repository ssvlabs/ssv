package proto

func (s *State) PreviouslyPrepared() bool {
	return s.PreparedRound != 0 && s.PreparedValue != nil
}
