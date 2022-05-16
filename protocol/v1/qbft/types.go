package qbft

import (
	"encoding/json"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"go.uber.org/atomic"
)

type RoundState int32

const (
	RoundState_NotStarted  RoundState = 0
	RoundState_PrePrepare  RoundState = 1
	RoundState_Prepare     RoundState = 2
	RoundState_Commit      RoundState = 3
	RoundState_ChangeRound RoundState = 4
	RoundState_Decided     RoundState = 5
	RoundState_Stopped     RoundState = 6
)

var RoundState_name = map[int32]string{
	0: "NotStarted",
	1: "PrePrepare",
	2: "Prepare",
	3: "Commit",
	4: "ChangeRound",
	5: "Decided",
	6: "Stopped",
}

var RoundState_value = map[string]int32{
	"NotStarted":  0,
	"PrePrepare":  1,
	"Prepare":     2,
	"Commit":      3,
	"ChangeRound": 4,
	"Decided":     5,
	"Stopped":     6,
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

func (s *State) GetHeight() message.Height {
	height := s.Height.Load().(message.Height)
	return height
}

func NewHeight(height message.Height) atomic.Value {
	h := atomic.Value{}
	h.Store(height)
	return h
}

func (s *State) GetRound() message.Round {
	round := s.Round.Load().(message.Round)
	return round
}

func NewRound(round message.Round) atomic.Value {
	value := atomic.Value{}
	value.Store(round)
	return value
}

func (s *State) GetPreparedRound() message.Round {
	round := s.PreparedRound.Load().(message.Round)
	return round
}

func (s *State) SetRound(newRound message.Round) {
	s.Round.Store(newRound)
}

func (s *State) GetIdentifier() message.Identifier {
	identifier := s.Identifier.Load().(message.Identifier)
	return identifier
}

func (s *State) GetInputValue() []byte {
	inputValue := s.InputValue.Load().([]byte)
	return inputValue
}

func (s *State) GetPreparedValue() []byte {
	value := s.PreparedValue.Load().([]byte)
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
