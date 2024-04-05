package cmd_debug_snapshot

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func TestGetFileNameWithoutExt(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "file with extension",
			path:     "/path/to/file.txt",
			expected: "file",
		},
		{
			name:     "file without extension",
			path:     "/path/to/file",
			expected: "file",
		},
		{
			name:     "file with multiple dots",
			path:     "/path/to/file.with.multiple.dots.txt",
			expected: "file.with.multiple.dots",
		},
		{
			name:     "empty string",
			path:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFileNameWithoutExt(tt.path)
			if result != tt.expected {
				t.Errorf("getFileNameWithoutExt(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestGetLogFilesAbsPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir) // clean up

	logFiles := []string{"test.log", "test2.log", "test3.log"}
	nonLogFiles := []string{"test.txt", "test.csv"}
	for _, file := range append(logFiles, nonLogFiles...) {
		_, err := os.Create(filepath.Join(tmpDir, file))
		require.NoError(t, err)
	}

	logFilePath := filepath.Join(tmpDir, "test.log")
	paths, err := getLogFilesAbsPaths(logFilePath)
	require.NoError(t, err)

	// Check that the function returns the correct paths to all the log files in the directory
	require.Len(t, paths, len(logFiles))
	for _, path := range paths {
		require.Contains(t, logFiles, filepath.Base(path))
	}
}

const superImportMetricName = "super_important_metric"

func runMetricsServ(port int) *http.Server {
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port)}

	http.Handle("/metrics", promhttp.Handler())

	yetAnotherCounter := promauto.NewCounter(prometheus.CounterOpts{
		Name: superImportMetricName,
		Help: "Don't forget to monitor this!",
	})

	for i := 0; i < 42; i++ {
		yetAnotherCounter.Inc()
	}
	return srv
}

func TestCollectMetrics(t *testing.T) {
	metricsPort := 2112
	srv := runMetricsServ(metricsPort)

	defer func() {
		require.NoError(t, srv.Close())
	}()
	go func() {
		err := srv.ListenAndServe()
		require.Equal(t, http.ErrServerClosed, err)
	}()

	dumpFilePath, err := dumpMetrics(fmt.Sprintf("http://localhost:%d/metrics", metricsPort), "metrics_dump.txt")
	require.NoError(t, err)

	metrics, err := parseMetrics(t, dumpFilePath)
	require.NoError(t, err)

	metricValue, found := metrics["super_important_metric"]
	require.True(t, found)
	require.Equal(t, "42", metricValue)

	// clean up
	err = os.Remove(dumpFilePath)
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

func deleteFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func generateLogFile(sizeInMB int, chunkSize int, path string) error {
	path = filepath.Clean(path)

	lumberjackLogger := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    chunkSize, // megabytes
		MaxBackups: 20,
		MaxAge:     28, // days
	}

	// Wrap the lumberjack.Logger in a zapcore.WriteSyncer
	syncWriter := zapcore.AddSync(lumberjackLogger)

	// Create a zapcore.Core that writes logs to the lumberjack.Logger
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		syncWriter,
		zap.InfoLevel,
	)

	logger := zap.New(core)

	logger.Info("Generating log file", zap.Int("sizeInMB", sizeInMB), zap.String("path", path))

	for i := 0; i < sizeInMB; i++ {
		logger.Info("Some dummy log", zap.String("data", generate1MBString()))
	}

	return nil
}

func unzipFile(zipFilePath string) (string, error) {
	destDir := getFileNameWithoutExt(zipFilePath)

	fmt.Println(zipFilePath, destDir)
	r, err := zip.OpenReader(filepath.Clean(zipFilePath + compressedFileExtension))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = r.Close()
	}()

	// Iterate through the files in the archive and extract them.
	for _, f := range r.File {
		err = extractFile(destDir, f)
		if err != nil {
			return "", err
		}
	}

	absPath, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}

	return absPath, nil
}

func extractFile(destDir string, f *zip.File) error {
	fpath := filepath.Join(destDir, f.Name)

	// Check for ZipSlip (Directory traversal vulnerability)
	if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
		log.Fatalf("illegal file path: %s", fpath)
	}

	// Create directory tree
	if f.FileInfo().IsDir() {
		err := os.MkdirAll(fpath, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to create sub directory: %w", err)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	defer func() {
		_ = outFile.Close()
	}()

	if err != nil {
		return err
	}

	rc, err := f.Open()

	if err != nil {

		return err
	}
	defer func() {
		_ = rc.Close()
	}()

	_, err = io.Copy(outFile, rc)
	if err != nil {
		return err
	}
	return nil
}

func generate1MBString() string {
	source := rand.NewSource(time.Now().UnixNano())
	rnd := rand.New(source)

	const MB = 1 << 20 // 1MB in bytes
	const charSize = 1 // size in bytes of the character we're appending

	// Create a string builder for efficient string concatenation
	var b strings.Builder
	// Preallocate enough memory for the final string
	b.Grow(MB)

	strSize := MB / charSize
	for i := 0; i < strSize; i++ {
		symbol := rnd.Intn('z'-'a') + 'a'
		b.WriteRune(rune(symbol))
	}

	return b.String()
}
