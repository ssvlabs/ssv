package runner

import (
	"context"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage/basedb"
)

// ScenarioFactory creates Scenario instances.
type ScenarioFactory func(name string) Scenario

// ScenarioContext is the context object that is passed in execution.
type ScenarioContext struct {
	Ctx         context.Context
	LocalNet    *p2pv1.LocalNet
	Stores      []*storage.QBFTStores
	KeyManagers []spectypes.KeyManager
	DBs         []basedb.IDb
}

// ScenarioConfig defines scenario properties.
type ScenarioConfig struct {
	Operators int
	BootNodes int
	FullNodes int
	Roles     []spectypes.BeaconRole
}

// Bootstrapper bootstraps the given scenario.
type Bootstrapper func(ctx context.Context, logger *zap.Logger, scenario Scenario) (*ScenarioContext, error)

// Scenario represents a testplan for a specific scenario
type Scenario interface {
	// Config returns defines properties of scenario.
	Config() ScenarioConfig
	// ApplyCtx applies scenario context returned by bootstrapper.
	ApplyCtx(sCtx *ScenarioContext)
	// Name is the name of the scenario
	Name() string
	// Run is the test scenario to run.
	Run(t *testing.T) error
}
