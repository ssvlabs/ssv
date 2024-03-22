package cmd_compress_logs

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
)

func TestCollectMetrics(t *testing.T) {
	srv := &http.Server{Addr: ":2112"}
	defer func() {
		require.NoError(t, srv.Close())
	}()
	http.Handle("/metrics", promhttp.Handler())

	go func() {
		err := srv.ListenAndServe()
		require.Equal(t, http.ErrServerClosed, err)
	}()

	yetAnotherCounter := promauto.NewCounter(prometheus.CounterOpts{
		Name: "super_important_metric",
		Help: "Don't forget to monitor this!",
	})

	for i := 0; i < 42; i++ {
		yetAnotherCounter.Inc()
	}
	collectMetricsArgs := CollectMetricsArgs{
		promRoute: "localhost:2112/metrics",
	}

	err := collectMetrics(&collectMetricsArgs)
	require.NoError(t, err)
	dumpFileName := "metrics_dump.txt"

	metrics, err := parseMetrics(t, dumpFileName)
	require.NoError(t, err)

	metricValue, found := metrics["super_important_metric"]
	require.True(t, found)
	require.Equal(t, "42", metricValue)

	// clean up
	err = os.Remove(dumpFileName)
	require.NoError(t, err)
}

func parseMetrics(t *testing.T, path string) (map[string]string, error) {
	f, err := os.Open(path)
	require.NoError(t, err)
	scanner := bufio.NewScanner(f)
	metrics := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Split the line into a key and a value
		parts := strings.SplitN(line, " ", 2)
		require.Equalf(t, 2, len(parts), fmt.Sprintf("invalid line: %s", line))

		key := parts[0]
		value := parts[1]

		metrics[key] = value
	}

	require.NoError(t, scanner.Err())

	return metrics, nil
}
