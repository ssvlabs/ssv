package cmd_compress_logs

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
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

	globalconfig "github.com/bloxapp/ssv/cli/config"
)

func TestGzipDir(t *testing.T) {
	path := filepath.Clean("/Users/dev/ssv/ssv/logs_xxx/test_logs_dir/test.log")
	dir := filepath.Dir(path)
	_, err := compressDirectory(dir, "x.gz")
	require.NoError(t, err)
}

func TestGetLogFiles(t *testing.T) {
	path := filepath.Clean("/Users/dev/ssv/ssv/logs_xxx/test_logs_dir/test.log")
	files, err := getLogFilesAbsPaths(path)
	fmt.Println(files)
	require.NoError(t, err)
}

func TestCompression(t *testing.T) {
	path := filepath.Clean("/Users/dev/ssv/ssv/logs_xxx/test_logs_dir/test.log")
	_, err := compressLogFiles(path)
	require.NoError(t, err)
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
	// Create a temporary file
	fileName := "test.log"
	err := generateLogFile(uint32(50), fileName)
	require.NoError(t, err)

	_, err = compressLogFiles(fileName)
	require.NoError(t, err)

	// Check that the compressed file exists
	//compressedFileName := file.Name() + compressedFileExtension
	//_, err = os.Stat(filepath.Clean(compressedFileName))
	//require.NoError(t, err)
	//
	//// Get the size of the compressed file
	//info, err = os.Stat(filepath.Clean(compressedFileName))
	//require.NoError(t, err)
	//compressedSize := info.Size()
	//
	//// Check that the compressed file is smaller than the original file
	//require.Less(t, compressedSize, originalSize)
	//
	//// Clean up the compressed file
	//_ = os.Remove(compressedFileName)
}

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
		SizeInMB int
	}{
		{50},
		//{10},
		//{500},
		//{1024},
	}

	for _, tc := range testCases {
		b.Run(fmt.Sprintf("%dMB", tc.SizeInMB), func(b *testing.B) {
			testLogFilePath := fmt.Sprintf("test_%dMB.log", tc.SizeInMB)

			gzPath := testLogFilePath + compressedFileExtension

			err := generateLogFile(uint32(tc.SizeInMB), testLogFilePath)
			require.NoError(b, err)

			_, err = compressLogFiles(testLogFilePath)
			require.NoError(b, err)

			info, err := os.Stat(gzPath)
			if err != nil {
				b.Fatal(err)
			}
			size := info.Size()

			// check size
			require.Lessf(b, float64(size)/float64(tc.SizeInMB*1024*1024), 0.2, "compressed file size is too large")

			// unzip and compare the files
			unzippedFileName := testLogFilePath + ".unzipped.log"
			err = unzip(testLogFilePath+compressedFileExtension, unzippedFileName)
			require.NoError(b, err)

			eq, err := compareFiles(testLogFilePath, unzippedFileName)
			require.NoError(b, err)
			require.True(b, eq, "files are not equal!")

			// delete files

			require.NoError(b, deleteFilesStartingWithPrefix(filepath.Dir(testLogFilePath), filepath.Base(testLogFilePath)))
			require.NoError(b, deleteFiles(gzPath, unzippedFileName))
		})
	}
}

func compareFiles(file1Path, file2Path string) (bool, error) {
	file1, err := os.Open(filepath.Clean(file1Path))
	if err != nil {
		return false, err
	}
	defer func() {
		_ = file1.Close()
	}()
	file2, err := os.Open(filepath.Clean(file2Path))
	if err != nil {
		return false, err
	}
	defer func() {
		_ = file2.Close()
	}()

	scanner1 := bufio.NewScanner(file1)
	scanner2 := bufio.NewScanner(file2)

	for scanner1.Scan() && scanner2.Scan() {
		if scanner1.Text() != scanner2.Text() {
			return false, nil
		}
	}

	if err := scanner1.Err(); err != nil {
		return false, err
	}
	if err := scanner2.Err(); err != nil {
		return false, err
	}

	// check both were completely read and have the same size
	if scanner1.Scan() != scanner2.Scan() {
		return false, nil
	}

	return true, nil
}

func generateLogFile(sizeInMB uint32, path string) error {
	// Create a logger
	path = filepath.Clean(path)
	logger, _ := zap.NewProduction()

	// Create a lumberjack.Logger
	lumberjackLogger := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    20, // megabytes
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

	// Create a zap.Logger that uses the zapcore.Core
	logger = zap.New(core)

	// Use the logger
	logger.Info("Generating log file", zap.Uint32("sizeInMB", sizeInMB), zap.String("path", path))

	// Write a large amount of data to the log file
	for i := uint32(0); i < sizeInMB; i++ {
		// Write another log message
		logger.Info("Some dummy log", zap.String("data", generate1MBString()))
	}

	return nil
}

func unzip(src string, dest string) error {
	// Open the gzip file
	r, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() {
		_ = r.Close()
	}()

	// Create a gzip reader
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() {
		_ = gr.Close()
	}()

	// Create destination file
	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		_ = destFile.Close()
	}()

	// Copy the gzip reader to the destination file
	_, err = io.Copy(destFile, gr)
	if err != nil {
		return err
	}

	return nil
}

func deleteFilesStartingWithPrefix(dirPath string, prefix string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), prefix) {
			if err := os.Remove(filepath.Join(dirPath, file.Name())); err != nil {
				return err
			}
		}
	}

	return nil
}

func deleteFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func generate1MBString() string {
	const MB = 1 << 20 // 1MB in bytes
	const charSize = 1 // size in bytes of the character we're appending

	// Create a string builder for efficient string concatenation
	var b strings.Builder
	// Preallocate enough memory for the final string
	b.Grow(MB)

	strSize := MB / charSize
	// Seed the random number generator
	rand.Seed(time.Now().UnixNano())

	for i := 0; i < strSize; i++ {
		symbol := rand.Intn('z'-'a') + 'a'
		b.WriteRune(rune(symbol))
	}

	return b.String()
}

// calcSizeOfFilesWithPrefix calculates the total size of files in a directory that have a specific prefix.
func calcSizeOfFilesWithPrefix(dirPath string, prefix string) (int64, error) {
	// Read the directory contents
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return 0, err
	}

	var totalSize int64
	for _, file := range files {
		// Check if the file name has the specified prefix
		if strings.HasPrefix(file.Name(), prefix) {
			// Get file information to access file size
			info, err := file.Info()
			if err != nil {
				return 0, err
			}
			totalSize += info.Size()
		}
	}

	// Optionally, print the total size in MB for debugging
	fmt.Printf("Total size of files with prefix '%s': %d bytes (%.2f MB)\n", prefix, totalSize, float64(totalSize)/1024.0/1024.0)

	return totalSize, nil
}
