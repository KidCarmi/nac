package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds the application's Prometheus collectors. They are registered
// against an injected registry so tests can use an isolated registry and
// avoid global-state collisions.
type Metrics struct {
	HTTPRequests   *prometheus.CounterVec
	HTTPDuration   *prometheus.HistogramVec
	AccessDecision *prometheus.CounterVec
	AuthFailures   *prometheus.CounterVec
	registry       *prometheus.Registry
}

// NewMetrics constructs and registers the collectors on a fresh registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	// Include Go runtime and process collectors for baseline observability.
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	factory := promauto.With(reg)
	return &Metrics{
		registry: reg,
		HTTPRequests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "nac_http_requests_total",
			Help: "Total HTTP requests by method, route, and status class.",
		}, []string{"method", "route", "status"}),
		HTTPDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "nac_http_request_duration_seconds",
			Help:    "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		AccessDecision: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "nac_access_decisions_total",
			Help: "Access decisions by resulting action.",
		}, []string{"action"}),
		AuthFailures: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "nac_auth_failures_total",
			Help: "Authentication failures by reason.",
		}, []string{"reason"}),
	}
}

// Registry exposes the underlying registry for the /metrics handler.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }
