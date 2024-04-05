package cmd_debug_snapshot

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

func TestCompressFile(t *testing.T) {
	logger := zap.NewNop()
	fileName := "test.log"
	err := generateLogFile(50, 20, fileName)
	require.NoError(t, err)

	zipName := "tmp_output"

	_, err = compressLogFiles(logger, &DebugSnapshotArgs{
		logFilePath: fileName,
		outputPath:  zipName,
	})
	require.NoError(t, err)

	unzippedPath, err := unzipFile(zipName)
	require.NoError(t, err)

	dir, err := os.ReadDir(unzippedPath)
	require.NoError(t, err)

	logFiles, err := getLogFilesAbsPaths(fileName)
	require.NoError(t, err)

	logFilesTotalSize := int64(0)
	for _, file := range logFiles {
		fi, err := os.Stat(file)
		require.NoError(t, err)
		logFilesTotalSize += fi.Size()
	}

	tarFilesTotalSize := int64(0)
	for _, file := range dir {
		fi, err := file.Info()
		require.NoError(t, err)
		tarFilesTotalSize += fi.Size()
	}

	require.Equal(t, logFilesTotalSize, tarFilesTotalSize)

	// clean up all
	require.NoError(t, os.RemoveAll(zipName))
	require.NoError(t, deleteFiles(zipName+compressedFileExtension))
	require.NoError(t, deleteFiles(logFiles...))
}

func BenchmarkDebugSnapshot(b *testing.B) {
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

			zipName := fmt.Sprintf("tmp_output_%dMB.log", tc.SizeInMB)
			_, err = compressLogFiles(logger, &DebugSnapshotArgs{
				logFilePath: testLogFilePath,
				outputPath:  zipName,
			})
			require.NoError(b, err)

			_, err = os.ReadFile("metrics_dump.txt")
			require.Error(b, err) // make sure collectLogFiles cleans up the metrics dump file

			// clean up all
			require.NoError(b, os.RemoveAll(zipName))
			require.NoError(b, deleteFiles(getFileNameWithoutExt(zipName)+compressedFileExtension))

			logFiles, err := getLogFilesAbsPaths(testLogFilePath)

			require.NoError(b, err)
			require.NoError(b, deleteFiles(logFiles...))
		})
	}
}
