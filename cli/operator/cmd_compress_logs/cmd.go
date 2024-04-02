package cmd_compress_logs

import (
	"archive/zip"
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

const compressedFileExtension = ".zip"

type CompressLogsArgs struct {
	logFilePath string
	outputPath  string
	upload      string
	clean       bool
	metrics     bool
}

var compressLogsArgs CompressLogsArgs

// CompressLogsCmd is the command to compress logs file with gzip
var CompressLogsCmd = &cobra.Command{
	Use:   "archive",
	Short: "Compresses internal logs into a zip file",
	Long: "Compresses internal logs into a zip file." +
		" The output file will be named after the provided destination name. " +
		"If no destination name is provided, the output file will be named after the input log file name with the current date and time appended to it.",
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
			zap.String("compressLogsArgs.outputPath", compressLogsArgs.outputPath),
		)

		fileSizeBytes, err := compressLogFiles(logger, &compressLogsArgs)

		if err != nil {
			logger.Fatal("logs file compression failed", zap.Error(err))
		}

		logger.Info("✅ logs compression finished", zap.Int64("file_size_bytes", fileSizeBytes))
	},
}

func compressLogFiles(logger *zap.Logger, args *CompressLogsArgs) (int64, error) {
	logFilePath := args.logFilePath
	destName := getFileNameWithoutExt(args.outputPath)

	if args.outputPath == "" {
		destName = getFileNameWithoutExt(logFilePath) + "_" + time.Now().Format(time.DateOnly) + "T" + time.Now().Format(time.TimeOnly)
	}

	// Find all log files in the directory of the provided file
	pathsToInclude, err := getLogFilesAbsPaths(logFilePath)
	if err != nil {
		return 0, err
	}

	if cfg.MetricsAPIPort > 0 {
		promRoute := fmt.Sprintf("http://localhost:%d/metrics", cfg.MetricsAPIPort)
		dumpFileName := "metrics_dump.txt"

		metricsDumpFileAbsPath, err := dumpMetrics(promRoute, dumpFileName)

		// Metrics are optional. Ignore if error, else include the metrics dump to the zip file
		if err != nil {
			logger.Error("failed to dump metrics", zap.Error(err))
		} else {
			pathsToInclude = append(pathsToInclude, metricsDumpFileAbsPath)
			defer func() {
				_ = os.Remove(dumpFileName)
			}()
		}
	}

	// Create a new zip file
	zipFile, err := os.Create(filepath.Clean(destName + compressedFileExtension))
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = zipFile.Close()
	}()
	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		_ = zipWriter.Close()
	}()

	// #TODO
	// Write concatenated debug log.
	//debugFile, _ := zipWriter.Create(`debug.log`)
	//for _, logFilename := filepath.Match(filepath.Join(filepath.Dir(logFilePath)), `debug*.log`)) {
	//	err := addFileToZip(logFilename, )
	//	fmt.Fprintf(debugFile, "\n") // separate log files in case they dont end with a newline
	//}
	//

	for _, path := range pathsToInclude {
		if err := addFileToZip(zipWriter, path); err != nil {
			return 0, err
		}
	}

	zipSize, err := calcFileSize(zipFile.Name())
	if err != nil {
		return 0, err
	}

	return zipSize, nil
}

func addFileToZip(zipWriter *zip.Writer, filePath string) error {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Create a header based on file info
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("failed to create header for file: %w", err)
	}
	header.Name = filepath.Base(filePath) // Use only the base name of the file
	header.Method = zip.Deflate           // Use deflate to compress the data

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to create writer for file: %w", err)
	}

	if _, err = io.Copy(writer, file); err != nil {
		return fmt.Errorf("failed to copy file data to zip: %w", err)
	}
	return nil
}

func dumpMetrics(promRoute string, metricsDumpFileName string) (string, error) {
	// #nosec G107
	resp, err := http.Get(promRoute)
	if err != nil {
		return "", fmt.Errorf("failed to get metrics from route %s with error: %w", promRoute, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	file, err := os.Create(filepath.Clean(metricsDumpFileName))
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
		&compressLogsArgs.outputPath,
		"out", "o", "", "output destination file name",
	)
	CompressLogsCmd.Flags().StringVarP(
		&compressLogsArgs.upload,
		"upload", "u", "", "URL for files uploading to S3",
	)
	CompressLogsCmd.Flags().BoolVarP(
		&compressLogsArgs.clean,
		"clean", "c", true, "remove .zip file after uploading it to S3",
	)
	CompressLogsCmd.Flags().BoolVarP(
		&compressLogsArgs.metrics,
		"metrics", "m", true, "try to collect metrics dump from the /metrics",
	)

	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, CompressLogsCmd)
}
