// Package metrics registers the Prometheus metrics that the operator and proxy
// expose. All metrics are registered against the default registerer so they
// appear on the controller-runtime /metrics endpoint automatically.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// InvocationTotal counts Bedrock invocations by endpoint and outcome.
	// Labels: namespace, endpoint (ModelEndpoint name), status (success|error).
	InvocationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "bedrock",
			Name:      "invocations_total",
			Help:      "Total Bedrock invocations routed through the proxy, by endpoint and outcome.",
		},
		[]string{"namespace", "endpoint", "model_id", "status"},
	)

	// InputTokensTotal counts input tokens consumed per endpoint.
	InputTokensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "bedrock",
			Name:      "input_tokens_total",
			Help:      "Cumulative input tokens sent to Bedrock, by endpoint.",
		},
		[]string{"namespace", "endpoint", "model_id"},
	)

	// OutputTokensTotal counts output tokens generated per endpoint.
	OutputTokensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "bedrock",
			Name:      "output_tokens_total",
			Help:      "Cumulative output tokens received from Bedrock, by endpoint.",
		},
		[]string{"namespace", "endpoint", "model_id"},
	)

	// InvocationCostDollars accumulates estimated USD spend per endpoint.
	// This is a counter (not a gauge) so Prometheus rate() works correctly.
	InvocationCostDollars = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "bedrock",
			Name:      "invocation_cost_dollars_total",
			Help:      "Estimated cumulative USD cost of Bedrock invocations, by endpoint.",
		},
		[]string{"namespace", "endpoint", "model_id"},
	)

	// InvocationDurationSeconds tracks end-to-end latency from the proxy's
	// perspective (receive request to forward response to caller).
	InvocationDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "bedrock",
			Name:      "invocation_duration_seconds",
			Help:      "End-to-end latency for Bedrock invocations through the proxy.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
		[]string{"namespace", "endpoint", "model_id"},
	)

	// RateLimitedTotal counts requests rejected by the token-bucket rate limiter.
	RateLimitedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "proxy",
			Name:      "rate_limited_total",
			Help:      "Requests rejected by the per-endpoint rate limiter.",
		},
		[]string{"namespace", "endpoint"},
	)

	// BudgetBreachedTotal counts requests rejected because a cost budget was exceeded.
	BudgetBreachedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "proxy",
			Name:      "budget_breached_total",
			Help:      "Requests rejected because the endpoint's cost budget was exceeded.",
		},
		[]string{"namespace", "endpoint", "period"},
	)

	// ReconcileErrorsTotal counts controller reconciliation errors by resource type.
	ReconcileErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "controller",
			Name:      "reconcile_errors_total",
			Help:      "Reconciliation errors by controller and error reason.",
		},
		[]string{"controller", "reason"},
	)

	// EndpointAvailable is a gauge (0 or 1) per ModelEndpoint reflecting the
	// BedrockReachable condition. Useful for alerting on endpoint outages.
	EndpointAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "k8s_ai_operator",
			Subsystem: "bedrock",
			Name:      "endpoint_available",
			Help:      "Whether the ModelEndpoint is available (1) or not (0).",
		},
		[]string{"namespace", "endpoint", "model_id"},
	)
)

func init() {
	// Register with the controller-runtime metrics registry so they appear
	// on the /metrics endpoint the manager exposes.
	metrics.Registry.MustRegister(
		InvocationTotal,
		InputTokensTotal,
		OutputTokensTotal,
		InvocationCostDollars,
		InvocationDurationSeconds,
		RateLimitedTotal,
		BudgetBreachedTotal,
		ReconcileErrorsTotal,
		EndpointAvailable,
	)
}
