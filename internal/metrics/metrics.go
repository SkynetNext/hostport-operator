package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// PortAllocationsTotal counts the total number of port allocations by policy
	PortAllocationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hostport_allocations_total",
			Help: "Total number of port allocations by policy",
		},
		[]string{"policy", "protocol"},
	)

	// PortAllocationErrorsTotal counts the total number of port allocation errors
	PortAllocationErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hostport_allocation_errors_total",
			Help: "Total number of port allocation errors",
		},
		[]string{"policy", "error_type"},
	)

	// PortConflictsTotal counts the total number of port conflicts detected
	PortConflictsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hostport_conflicts_total",
			Help: "Total number of port conflicts detected",
		},
		[]string{"node", "protocol"},
	)

	// PortAllocationDurationSeconds measures the duration of port allocation operations
	PortAllocationDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "hostport_allocation_duration_seconds",
			Help:    "Duration of port allocation operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy"},
	)

	// WebhookRequestsTotal counts the total number of webhook requests
	WebhookRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hostport_webhook_requests_total",
			Help: "Total number of webhook requests",
		},
		[]string{"result"}, // result: "allowed", "denied", "errored"
	)
)
