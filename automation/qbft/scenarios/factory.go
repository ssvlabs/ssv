package scenarios

import (
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"go.uber.org/zap"
	"sync"
)

// loggable marks a struct as a component with logger
type loggable interface {
	// setLogger enables to inject a logger instance
	setLogger(l *zap.Logger)
}

var scenarios = make(map[string]runner.Scenario)

var once sync.Once

// NewScenario is a factory function to create scenarios
func NewScenario(name string, logger *zap.Logger) runner.Scenario {
	once.Do(func() {
		s := &onForkV1{}
		scenarios[s.Name()] = s
		// TODO: add other scenarios
	})
	if s, ok := scenarios[name]; ok {
		// configure logger if possible
		if ls, ok := s.(loggable); ok {
			ls.setLogger(logger)
		}
		return s
	}
	return nil
}
