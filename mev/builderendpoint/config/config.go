package config

import "fmt"

// Config defines configuration for the SSV-hosted Builder API endpoint.
//
// This is intentionally a standalone top-level config tree (see `yaml:"builder"` in `cli/operator/node.go`)
// to avoid mixing it with beacon/EL/SSV configs.
type Config struct {
	Enabled       bool   `yaml:"Enabled" env:"ENABLED" env-default:"false" env-description:"Enable the SSV-hosted Builder API endpoint (mev-boost-compatible)"`
	ListenAddress string `yaml:"ListenAddress" env:"LISTEN_ADDRESS" env-description:"Listen address for the builder endpoint (e.g. 127.0.0.1:18550)"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.ListenAddress == "" {
		return fmt.Errorf("builder endpoint enabled but ListenAddress is empty")
	}
	return nil
}
