package metrics

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

// ReportSSVNodeHealthiness reports SSV node healthiness.
func ReportSSVNodeHealthiness(healthy bool) {
	if healthy {
		metricsNodeStatus.Set(float64(statusHealthy))
	} else {
		metricsNodeStatus.Set(float64(statusNotHealthy))
	}
}
