package qbft

import (
	"sync"

	"github.com/bloxapp/ssv/protocol/v1/message"
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

func (r RoundState) String() string {
	if v, ok := RoundState_name[int32(r)]; ok {
		return v
	}

	return RoundState_name[0]
}

func (r RoundState) Int32() int32 {
	return int32(r)
}

// State holds an iBFT state, thread safe
type State struct {
	mu            sync.Mutex
	Stage         RoundState
	Identifier    message.Identifier // instance unique identifier, much like a block hash in a blockchain
	Height        message.Height     // incremental number for each instance, much like a block number would be in a blockchain
	InputValue    []byte
	Round         message.Round
	PreparedRound message.Round
	PreparedValue []byte
}

func (s *State) GetStage() RoundState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.Stage
}

func (s *State) GetHeight() message.Height {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.Height
}

func (s *State) GetRound() message.Round {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.Round
}

func (s *State) GetPreparedRound() message.Round {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.PreparedRound
}

func (s *State) GetInputValue() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.InputValue
}

func (s *State) GetPreparedValue() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.PreparedValue
}

func (s *State) GetIdentifier() message.Identifier {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.Identifier
}

func (s *State) SetRound(newRound message.Round) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Round = newRound
}

func (s *State) SetStage(newStage RoundState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Stage = newStage
}

func (s *State) SetInputValue(newValue []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.InputValue = newValue
}

func (s *State) SetPreparedRound(newRound message.Round) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PreparedRound = newRound
}

func (s *State) SetPreparedValue(newValue []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PreparedValue = newValue
}

func (s *State) SetIdentifier(identifier message.Identifier) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Identifier = identifier
}

func (s *State) SetHeight(height message.Height) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Height = height
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
