package scenarios

import (
	"sync"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/qbft/runner"
)

var scenarios = &sync.Map{}

// NewScenario is a factory function to get or create scenarios
func NewScenario(name string, logger *zap.Logger) runner.Scenario {
	var s runner.Scenario
	raw, ok := scenarios.Load(name)
	if !ok {
		switch name {
		case RegularScenario:
			s = newRegularScenario(logger)
		default:
			logger.Panic("could not find scenario")
		}
		if s != nil {
			scenarios.Store(s.Name(), s)
			return s
		}
		return nil
	}
	return raw.(runner.Scenario)
}
