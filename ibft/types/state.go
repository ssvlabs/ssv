package types

type State struct {
	IBFTId        uint64
	Lambda        []byte
	InputValue    []byte
	Round         uint64
	PreparedRound uint64
	PreparedValue []byte
}
