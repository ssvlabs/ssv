package cmd_compress_logs

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	globalconfig "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/utils/commons"
)

type config struct {
	globalconfig.GlobalConfig `yaml:"global"`
	MetricsAPIPort            int `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"Port to listen on for the metrics API."`
}

var cfg config
var globalArgs globalconfig.Args

const compressedFileExtension = ".gz"

type CompressLogsArgs struct {
	logFilePath string
	destName    string
}

var compressLogsArgs CompressLogsArgs

// CompressLogsCmd is the command to compress logs file with gzip
var CompressLogsCmd = &cobra.Command{
	Use:   "compress-logs",
	Short: "Compresses logs",
	Run: func(cmd *cobra.Command, args []string) {
		logger, err := setupGlobal(&cfg)

		if err != nil {
			logger.Fatal("initialization failed", zap.Error(err))
		}

		if compressLogsArgs.logFilePath == "" {
			compressLogsArgs.logFilePath = cfg.GlobalConfig.LogFilePath
		}

		logger.Info("starting logs compression",
			zap.Any("buildData", commons.GetBuildData()),
			zap.String("compressLogsArgs.logFilePath", compressLogsArgs.logFilePath),
			zap.String("compressLogsArgs.destName", compressLogsArgs.destName),
		)

		fileSizeBytes, err := compressLogFiles(logger, &compressLogsArgs)

		if err != nil {
			logger.Fatal("logs file compression failed", zap.Error(err))
		}

		logger.Info("✅ logs compression finished", zap.Int64("file_size_bytes", fileSizeBytes))
	},
}

const tmpDirPermissions os.FileMode = 0755

func compressLogFiles(logger *zap.Logger, args *CompressLogsArgs) (int64, error) {
	logFilePath := args.logFilePath
	destName := getFileNameWithoutExt(args.destName)

	if args.destName == "" {
		destName = getFileNameWithoutExt(logFilePath) + "_" + time.Now().Format(time.DateOnly) + "T" + time.Now().Format(time.TimeOnly)
	}

	// Find all log files in the directory of the provided file
	pathToInclude, err := getLogFilesAbsPaths(logFilePath)
	if err != nil {
		return 0, err
	}

	if cfg.MetricsAPIPort > 0 {
		promRoute := fmt.Sprintf("http://localhost:%d/metrics", cfg.MetricsAPIPort)
		dumpFileName := "metrics_dump.txt"

		metricsDumpFileAbsPath, err := dumpMetrics(promRoute, dumpFileName)

		// Metrics are optional. Ignore if error, else include the metrics dump to the tar file
		if err != nil {
			logger.Error("failed to dump metrics", zap.Error(err))
		} else {
			pathToInclude = append(pathToInclude, metricsDumpFileAbsPath)
			defer func() {
				_ = os.Remove(dumpFileName)
			}()
		}
	}

	// Create a temporary directory
	err = os.Mkdir(destName, tmpDirPermissions)

	if err != nil {
		return 0, err
	}

	// clean up the tmp dir
	defer func() {
		_ = os.RemoveAll(destName)
	}()

	err = copyFilesToDir(destName, pathToInclude)
	if err != nil {
		return 0, fmt.Errorf("copyFilesToDir failed: %w", err)
	}

	tarAbsPath, err := compressDirectory(destName)
	if err != nil {
		return 0, fmt.Errorf("compressDirectory failed: %w", err)
	}

	tarSize, err := calcFileSize(tarAbsPath)
	if err != nil {
		return 0, err
	}

	return tarSize, nil
}

func dumpMetrics(promRoute string, metricsDumpFileName string) (string, error) {
	resp, err := http.Get(promRoute)
	if err != nil {
		return "", fmt.Errorf("failed to get metrics from route %s with error: %w", promRoute, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	file, err := os.Create(metricsDumpFileName)
	if err != nil {
		return "", fmt.Errorf("failed to create dump file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write to file: %w", err)
	}
	absPath, err := filepath.Abs(file.Name())
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for file: %w", err)
	}
	return absPath, nil
}

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, CompressLogsCmd)

	CompressLogsCmd.Flags().StringVarP(
		&compressLogsArgs.destName,
		"out", "o", "", "output destination file name",
	)

	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, CompressLogsCmd)
}
