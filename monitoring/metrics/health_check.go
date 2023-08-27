package metrics

// HealthChecker represent an health-check agent
type HealthChecker interface {
	HealthCheck() error
}
