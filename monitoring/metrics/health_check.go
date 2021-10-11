package metrics

import (
	"go.uber.org/zap"
	"time"
)

// HealthCheckAgent represent an health-check agent
type HealthCheckAgent interface {
	HealthCheck() []string
}

// ProcessAgents takes a slice of HealthCheckAgent, and invokes them
func ProcessAgents(agents []HealthCheckAgent) []string {
	var errs []string

	// health checks from all agents
	for _, agent := range agents {
		if agentErrs := agent.HealthCheck(); len(agentErrs) > 0 {
			errs = append(errs, agentErrs...)
		}
	}

	return errs
}

// WaitUntilHealthy takes some component (that implements HealthCheckAgent) and wait until it is healthy
func WaitUntilHealthy(logger *zap.Logger, component interface{}, name string) {
	agent, ok := component.(HealthCheckAgent)
	if !ok {
		logger.Warn("component does not implement HealthCheckAgent interface")
		return
	}
	for {
		errs := agent.HealthCheck()
		if len(errs) == 0 {
			break
		}
		logger.Warn(name + " is not healthy, trying again in 1sec")
		time.Sleep(1 * time.Second)
	}
	logger.Debug(name + " is healthy")
}
