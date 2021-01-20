package types

type State struct {
	Lambda        interface{}
	Round         uint64
	PreparedRound uint64
	PreparedValue interface{}
	InputValue    interface{}
}
