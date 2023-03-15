package metrics

import (
	"time"

	"github.com/bloxapp/ssv/logging/fields"

	"go.uber.org/zap"
)

// HealthCheckAgent represent an health-check agent
type HealthCheckAgent interface {
	HealthCheck() []error
}

// ProcessAgents takes a slice of HealthCheckAgent, and invokes them
func ProcessAgents(agents []HealthCheckAgent) []error {
	var errs []error

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
		logger.Warn(name+" is not healthy, trying again in 1sec", fields.Errors(errs))
		time.Sleep(1 * time.Second)
	}
	logger.Debug(name + " is healthy")
}

// ReportSSVNodeHealthiness reports SSV node healthiness.
func ReportSSVNodeHealthiness(healthy bool) {
	if healthy {
		metricsNodeStatus.Set(float64(statusHealthy))
	} else {
		metricsNodeStatus.Set(float64(statusNotHealthy))
	}
}
