package scenarios

import (
	"context"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

type ScenarioFactory func(name string) Scenario

type ScenarioContext struct {
	Ctx         context.Context
	LocalNet    *p2pv1.LocalNet
	Stores      []qbftstorage.QBFTStore
	KeyManagers []beacon.KeyManager
}

type scenarioCfg interface {
	NumOfOperators() int
	NumOfExporters() int
}

type Scenario interface {
	scenarioCfg

	Name() string
	PreExecution(ctx *ScenarioContext) error
	Execute(ctx *ScenarioContext) error
	PostExecution(ctx *ScenarioContext) error
}
