package main

import (
	"github.com/bloxapp/ssv/networkconfig"
)

type Config struct {
	Global struct {
		LogLevel string `yaml:"LogLevel,omitempty"`
	} `yaml:"global,omitempty"`
	DB struct {
		Path string `yaml:"Path,omitempty"`
	} `yaml:"db,omitempty"`
	ConsensusClient struct {
		Address string `yaml:"BeaconNodeAddr,omitempty"`
	} `yaml:"eth2,omitempty"`
	ExecutionClient struct {
		Address string `yaml:"ETH1Addr,omitempty"`
	} `yaml:"eth1,omitempty"`
	P2P struct {
		Discovery string `yaml:"Discovery,omitempty"`
	} `yaml:"p2p,omitempty"`
	SSV struct {
		NetworkName   string                       `yaml:"Network,omitempty" env:"NETWORK" env-description:"Network is the network of this node,omitempty"`
		CustomNetwork *networkconfig.NetworkConfig `yaml:"CustomNetwork,omitempty" env:"CUSTOM_NETWORK" env-description:"Custom network parameters,omitempty"`
	} `yaml:"ssv,omitempty"`
	OperatorPrivateKey string `yaml:"OperatorPrivateKey,omitempty"`
	MetricsAPIPort     int    `yaml:"MetricsAPIPort,omitempty"`
}
