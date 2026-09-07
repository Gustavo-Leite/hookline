package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Gustavo-Leite/hookline/internal/metrics"
)

func Metrics() http.Handler {
	return promhttp.Handler()
}

func Instrument(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		metrics.HTTPRequests.WithLabelValues(route, strconv.Itoa(recorder.status)).Inc()
		metrics.HTTPDuration.WithLabelValues(route).Observe(time.Since(started).Seconds())
	})
}
