package queue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics records metrics about the Queue.
type Metrics interface {
	// Dropped increments the number of messages dropped from the Queue.
	Dropped()
}

type queueWithMetrics struct {
	Queue
	metrics Metrics
}

// WithMetrics returns a wrapping of the given Queue that records metrics using the given Metrics.
func WithMetrics(q Queue, metrics Metrics) Queue {
	return &queueWithMetrics{
		Queue:   q,
		metrics: metrics,
	}
}

func (q *queueWithMetrics) TryPush(msg *DecodedSSVMessage) bool {
	pushed := q.Queue.TryPush(msg)
	if !pushed {
		q.metrics.Dropped()
	}
	return pushed
}

// TODO: move to metrics/prometheus package
type prometheusMetrics struct {
	dropped prometheus.Counter
}

// NewPrometheusMetrics returns a Prometheus implementation of Metrics.
func NewPrometheusMetrics(messageID string) Metrics {
	return &prometheusMetrics{
		dropped: metricMessageDropped.WithLabelValues(messageID),
	}
}

func (m *prometheusMetrics) Dropped() {
	m.dropped.Inc()
}

// Register Prometheus metrics.
var (
	metricMessageDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:ibft:msgq:drops",
		Help: "The amount of message dropped from the validator's msg queue",
	}, []string{"msg_id"})
)

func init() {
	_ = prometheus.Register(metricMessageDropped)
}
