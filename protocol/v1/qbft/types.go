package qbft

import (
	"encoding/json"

	"go.uber.org/atomic"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

// RoundState is the state of the round
type RoundState int32

// RoundState values
const (
	RoundStateNotStarted  RoundState = 0
	RoundStatePrePrepare  RoundState = 1
	RoundStatePrepare     RoundState = 2
	RoundStateCommit      RoundState = 3
	RoundStateChangeRound RoundState = 4
	RoundStateDecided     RoundState = 5
	RoundStateStopped     RoundState = 6
)

// RoundStateName represents the map of the round state names
var RoundStateName = map[int32]string{
	0: "NotStarted",
	1: "PrePrepare",
	2: "Prepare",
	3: "Commit",
	4: "ChangeRound",
	5: "Decided",
	6: "Stopped",
}

// State holds an iBFT state, thread safe
type State struct {
	Stage atomic.Int32 // RoundState
	// lambda is an instance unique identifier, much like a block hash in a blockchain
	Identifier atomic.Value // message.Identifier
	// Height is an incremental number for each instance, much like a block number would be in a blockchain
	Height        atomic.Value // message.Height
	InputValue    atomic.Value // []byte
	Round         atomic.Value // message.Round
	PreparedRound atomic.Value // message.Round
	PreparedValue atomic.Value // []byte
}

type unsafeState struct {
	Stage         int32
	Identifier    message.Identifier
	Height        message.Height
	InputValue    []byte
	Round         message.Round
	PreparedRound message.Round
	PreparedValue []byte
}

// MarshalJSON implements marshaling interface
func (s *State) MarshalJSON() ([]byte, error) {
	return json.Marshal(&unsafeState{
		Stage:         s.Stage.Load(),
		Identifier:    s.GetIdentifier(),
		Height:        s.GetHeight(),
		InputValue:    s.GetInputValue(),
		Round:         s.GetRound(),
		PreparedRound: s.GetPreparedRound(),
		PreparedValue: s.GetPreparedValue(),
	})
}

// UnmarshalJSON implements marshaling interface
func (s *State) UnmarshalJSON(data []byte) error {
	d := &unsafeState{}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}

	s.Stage.Store(d.Stage)
	s.Identifier.Store(d.Identifier)
	s.Height.Store(d.Height)
	s.InputValue.Store(d.InputValue)
	s.Round.Store(d.Round)
	s.PreparedRound.Store(d.PreparedRound)
	s.PreparedValue.Store(d.PreparedValue)

	return nil
}

// GetHeight returns the height of the state
func (s *State) GetHeight() message.Height {
	if height, ok := s.Height.Load().(message.Height); ok {
		return height
	}

	return message.Height(0)
}

// NewHeight returns a new height
func NewHeight(height message.Height) atomic.Value {
	h := atomic.Value{}
	h.Store(height)
	return h
}

// GetRound returns the round of the state
func (s *State) GetRound() message.Round {
	if round, ok := s.Round.Load().(message.Round); ok {
		return round
	}
	return message.Round(0)
}

// NewRound returns a new round
func NewRound(round message.Round) atomic.Value {
	value := atomic.Value{}
	value.Store(round)
	return value
}

// GetPreparedRound returns the prepared round of the state
func (s *State) GetPreparedRound() message.Round {
	if round, ok := s.PreparedRound.Load().(message.Round); ok {
		return round
	}

	return message.Round(0)
}

// GetIdentifier returns the identifier of the state
func (s *State) GetIdentifier() message.Identifier {
	if identifier, ok := s.Identifier.Load().(message.Identifier); ok {
		return identifier
	}

	return nil
}

// GetInputValue returns the input value of the state
func (s *State) GetInputValue() []byte {
	if inputValue, ok := s.InputValue.Load().([]byte); ok {
		return inputValue
	}
	return nil
}

// GetPreparedValue returns the prepared value of the state
func (s *State) GetPreparedValue() []byte {
	if value, ok := s.PreparedValue.Load().([]byte); ok {
		return value
	}

	return nil
}

// NewByteValue returns a new byte value
func NewByteValue(val []byte) atomic.Value {
	value := atomic.Value{}
	value.Store(val)
	return value
}

// InstanceConfig is the configuration of the instance
type InstanceConfig struct {
	RoundChangeDurationSeconds   float32
	LeaderPreprepareDelaySeconds float32
}

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *InstanceConfig {
	return &InstanceConfig{
		RoundChangeDurationSeconds:   3,
		LeaderPreprepareDelaySeconds: 1,
	}
}
