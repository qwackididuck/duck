// Package metrics provides HTTP metrics implementations for use with the
// [middleware.HTTPMetricsProvider] interface.
//
// The Prometheus implementation exposes a /metrics handler and records
// per-request counters and histograms.
//
// Usage:
//
//	m, err := metrics.NewPrometheus("myapp",
//	    metrics.WithConstLabels(prometheus.Labels{"env": "prod"}),
//	    metrics.WithAdditionalLabels("tenant"),
//	)
//
//	router.Handle("/metrics", m.Handler())
//	router.Use(middleware.HTTPMetrics(m))
package metrics

import (
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	labelRoute      = "route"
	labelMethod     = "method"
	labelStatusCode = "status_code"
)

// defaultBuckets are the default histogram buckets for request duration.
// Covers 1ms to 10s with reasonable granularity.
var defaultBuckets = []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// PrometheusOption is a functional option for [Prometheus].
type PrometheusOption func(*prometheusOptions)

type prometheusOptions struct {
	constLabels      prometheus.Labels
	additionalLabels []string
	buckets          []float64
	registry         *prometheus.Registry
}

// WithConstLabels sets constant labels applied to all metrics.
// Use this for labels that are the same for the entire process lifetime
// (e.g. env, region, version).
func WithConstLabels(labels prometheus.Labels) PrometheusOption {
	return func(o *prometheusOptions) {
		o.constLabels = labels
	}
}

// WithAdditionalLabels declares extra per-request variable label names.
// The values must be provided at request time via
// [middleware.WithLabelsFromRequest].
//
// The label names declared here must exactly match the keys returned by
// the WithLabelsFromRequest function, otherwise Prometheus will panic.
func WithAdditionalLabels(labels ...string) PrometheusOption {
	return func(o *prometheusOptions) {
		o.additionalLabels = labels
	}
}

// WithBuckets sets custom histogram buckets for request duration.
// Defaults to [defaultBuckets].
func WithBuckets(buckets []float64) PrometheusOption {
	return func(o *prometheusOptions) {
		o.buckets = buckets
	}
}

// WithRegistry sets a custom Prometheus registry.
// Defaults to a new isolated [prometheus.Registry].
// Pass prometheus.DefaultRegisterer to use the global registry.
func WithRegistry(reg *prometheus.Registry) PrometheusOption {
	return func(o *prometheusOptions) {
		o.registry = reg
	}
}

// Prometheus implements [middleware.HTTPMetricsProvider] using Prometheus.
// It records a counter and a histogram per request.
type Prometheus struct {
	registry *prometheus.Registry
	total    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewPrometheus creates and registers HTTP metrics collectors.
// namespace is used as the Prometheus metric namespace
// (e.g. "myapp" produces "myapp_http_requests_total").
func NewPrometheus(namespace string, opts ...PrometheusOption) (*Prometheus, error) {
	o := &prometheusOptions{
		buckets:  defaultBuckets,
		registry: prometheus.NewRegistry(),
	}

	for _, opt := range opts {
		opt(o)
	}

	labels := []string{labelRoute, labelMethod, labelStatusCode}
	labels = append(labels, o.additionalLabels...)

	total := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   "http",
		Name:        "requests_total",
		Help:        fmt.Sprintf("Total number of HTTP requests handled by %s.", namespace),
		ConstLabels: o.constLabels,
	}, labels)

	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   "http",
		Name:        "request_duration_seconds",
		Help:        fmt.Sprintf("Duration of HTTP requests handled by %s in seconds.", namespace),
		Buckets:     o.buckets,
		ConstLabels: o.constLabels,
	}, labels)

	if err := o.registry.Register(total); err != nil {
		return nil, fmt.Errorf("register requests_total: %w", err)
	}

	if err := o.registry.Register(duration); err != nil {
		return nil, fmt.Errorf("register request_duration_seconds: %w", err)
	}

	return &Prometheus{
		registry: o.registry,
		total:    total,
		duration: duration,
	}, nil
}

// NotifyServerExchange implements [middleware.HTTPMetricsProvider].
func (p *Prometheus) NotifyServerExchange(
	statusCode int,
	route, method string,
	dur time.Duration,
	additionalLabels map[string]string,
) {
	labels := prometheus.Labels{
		labelRoute:      route,
		labelMethod:     method,
		labelStatusCode: strconv.Itoa(statusCode),
	}

	maps.Copy(labels, additionalLabels)

	p.total.With(labels).Inc()
	p.duration.With(labels).Observe(dur.Seconds())
}

// Handler returns an HTTP handler that exposes the Prometheus metrics
// in the text exposition format. Mount this at /metrics.
func (p *Prometheus) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{})
}
