package beacon

import (
	"context"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
)

// Options for controller struct creation
type Options struct {
	Context        context.Context
	Logger         *zap.Logger
	Network        string `yaml:"Network" env:"NETWORK" env-default:"prater"`
	BeaconNodeAddr string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	Graffiti       []byte
	DB             basedb.IDb
}
