package validator

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

// Options represents options that should be passed to a new instance of Validator.
type Options struct {
	Logger      *zap.Logger
	Network     specqbft.Network
	Beacon      specssv.BeaconNode
	Storage     *storage.QBFTStores
	SSVShare    *types.SSVShare
	Signer      spectypes.KeyManager
	DutyRunners runner.DutyRunners
	Mode        Mode
	FullNode    bool
}

func (o *Options) defaults() {
	if o.Logger == nil {
		o.Logger = zap.L()
	}
}

// State of the validator
type State uint32

const (
	// NotStarted the validator hasn't started
	NotStarted State = iota
	// Started validator is running
	Started
)

// Mode defines a mode Validator operates in.
type Mode int32

const (
	// ModeRW for validators that we operate
	ModeRW Mode = iota
	// ModeR for non-committee validators
	ModeR
)
