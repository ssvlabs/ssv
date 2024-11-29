package goclient

import (
	"context"
	"time"
)

// Options for controller struct creation
type Options struct {
	Context        context.Context
	BeaconNodeAddr string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	GasLimit       uint64
	CommonTimeout  time.Duration // Optional.
	LongTimeout    time.Duration // Optional.
}
