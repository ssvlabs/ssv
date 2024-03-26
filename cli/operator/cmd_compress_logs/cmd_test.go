package cmd_compress_logs

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	globalconfig "github.com/bloxapp/ssv/cli/config"
)

func TestSetGlobalLogger(t *testing.T) {
	// Create a config object
	cfg := &config{
		GlobalConfig: globalconfig.GlobalConfig{
			LogLevel:       "debug",
			LogLevelFormat: "console",
		},
	}

	// Call the setGlobalLogger function
	logger, err := setGlobalLogger(cfg)

	// Check that the function does not return an error
	require.NoError(t, err)

	// Check that the function returns a valid logger
	require.IsType(t, &zap.Logger{}, logger)
}

func BenchmarkCompressLogs(b *testing.B) {
	testCases := []struct {
		SizeInMB    int
		ChunkSizeMB int
	}{
		{10, 10},
		{10, 100},
		{50, 20},
		{500, 20},
		{500, 100},
		{1024, 500},
	}

	cfg.MetricsAPIPort = 9093
	metricsSrv := runMetricsServ(9093)

	defer func() {
		require.NoError(b, metricsSrv.Close())
	}()
	go func() {
		err := metricsSrv.ListenAndServe()
		require.Equal(b, http.ErrServerClosed, err)
	}()

	for _, tc := range testCases {
		b.Run(fmt.Sprintf("%dMB", tc.SizeInMB), func(b *testing.B) {

			logger := zap.NewNop()
			testLogFilePath := fmt.Sprintf("test_%dMB.log", tc.SizeInMB)

			err := generateLogFile(tc.SizeInMB, tc.ChunkSizeMB, testLogFilePath)
			require.NoError(b, err)

			tarName := fmt.Sprintf("tmp_output_%dMB.log", tc.SizeInMB)
			_, err = compressLogFiles(logger, &CompressLogsArgs{
				logFilePath: testLogFilePath,
				destName:    tarName,
			})
			require.NoError(b, err)

			_, err = os.ReadFile("metrics_dump.txt")
			require.Error(b, err) // make sure collectLogFiles cleans up the metrics dump file

			// clean up all
			require.NoError(b, os.RemoveAll(tarName))
			require.NoError(b, deleteFiles(getFileNameWithoutExt(tarName)+compressedFileExtension))

			logFiles, err := getLogFilesAbsPaths(testLogFilePath)

			require.NoError(b, err)
			require.NoError(b, deleteFiles(logFiles...))
		})
	}
}
