package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	PolicyViolationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policy_violations_total",
			Help: "Total number of policy violations",
		},
		[]string{"policy", "resource", "namespace", "mode"},
	)

	PolicyEvaluationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policy_evaluations_total",
			Help: "Total number of policy evaluations",
		},
		[]string{"policy", "result"},
	)

	WebhookRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "webhook_request_duration_ms",
			Help:    "Webhook request latency in milliseconds",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200, 500},
		},
		[]string{"operation", "resource"},
	)

	PolicyErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policy_errors_total",
			Help: "Total number of policy evaluation errors",
		},
		[]string{"policy"},
	)

	ActivePolicies = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "active_policies",
			Help: "Number of currently active policies",
		},
		[]string{"mode"},
	)
)

// RecordActivePolicies updating the gauge metrics periodically
func RecordActivePolicies(policies map[string]string) {
	// Reset first
	ActivePolicies.Reset()

	counts := make(map[string]float64)
	for _, mode := range policies {
		counts[mode]++
	}

	for mode, count := range counts {
		ActivePolicies.WithLabelValues(mode).Set(count)
	}
}
