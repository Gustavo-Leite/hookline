package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "hookline"

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "HTTP requests handled, by route and status class.",
	}, []string{"route", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "How long the API took to answer, by route.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"route"})

	RateLimited = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "rate_limited_total",
		Help:      "Requests rejected because the API key exceeded its allowance.",
	})

	DeliveryAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "delivery",
		Name:      "attempts_total",
		Help:      "Delivery attempts, by what the attempt led to.",
	}, []string{"outcome"})

	DeliveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "delivery",
		Name:      "duration_seconds",
		Help:      "How long the receiver took to answer.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})

	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "queue",
		Name:      "pending_deliveries",
		Help:      "Deliveries waiting to be claimed right now.",
	})
)
