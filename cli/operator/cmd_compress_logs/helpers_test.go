package cmd_compress_logs

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestCopyFilesToDir(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "src")
	require.NoError(t, err)
	defer os.RemoveAll(srcDir) // clean up

	files := []string{"test1.txt", "test2.txt", "test3.txt"}
	for _, file := range files {
		_, err := os.Create(filepath.Join(srcDir, file))
		require.NoError(t, err)
	}

	// Create a temporary destination directory
	destDir, err := os.MkdirTemp("", "dest")
	require.NoError(t, err)

	defer os.RemoveAll(destDir) // clean up

	// Get the absolute paths of the source files
	var srcFiles []string
	for _, file := range files {
		srcFiles = append(srcFiles, filepath.Join(srcDir, file))
	}

	err = copyFilesToDir(destDir, srcFiles)
	require.NoError(t, err)

	// Check that the function has copied all the files from the source to the destination directory
	destFiles, err := os.ReadDir(destDir)
	require.NoError(t, err)
	require.Len(t, destFiles, len(files))
	for _, file := range destFiles {
		require.Contains(t, files, file.Name())
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

func TestCompressFile(t *testing.T) {
	fileName := "test.log"
	err := generateLogFile(50, 20, fileName)
	require.NoError(t, err)

	tarName := "tmp_output"

	_, err = compressLogFiles(&CompressLogsArgs{logFilePath: fileName, destName: tarName})
	require.NoError(t, err)

	untarPath, err := untarGzFile(tarName)
	require.NoError(t, err)

	dir, err := os.ReadDir(untarPath)
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
	require.NoError(t, os.RemoveAll(tarName))
	require.NoError(t, deleteFiles(tarName+compressedFileExtension))
	require.NoError(t, deleteFiles(logFiles...))
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
		// Write another log message
		logger.Info("Some dummy log", zap.String("data", generate1MBString()))
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
