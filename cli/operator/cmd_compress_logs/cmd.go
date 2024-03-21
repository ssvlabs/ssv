package cmd_compress_logs

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	globalconfig "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/utils/commons"
)

type config struct {
	globalconfig.GlobalConfig `yaml:"global"`
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

		logger.Info(fmt.Sprintf("starting logs compression %v", commons.GetBuildData()))

		logFilePath := cfg.LogFilePath

		//compressedFileName := filepath.Base(logFilePath) + "_" + time.Now().Format(time.RFC3339) + compressedFileExtension
		fileSizeBytes, err := compressLogFiles(&CompressLogsArgs{
			logFilePath: logFilePath,
			//destName:    compressedFileName,
		})

		if err != nil {
			logger.Fatal("logs file compression failed", zap.Error(err))
		}

		logger.Info("✅ logs compression finished", zap.Int64("file_size_bytes", fileSizeBytes))
	},
}

const tmpDirPermissions os.FileMode = 0755

func compressLogFiles(args *CompressLogsArgs) (int64, error) {
	logFilePath := args.logFilePath
	destName := getFileNameWithoutExt(args.destName)

	if args.destName == "" {
		destName = getFileNameWithoutExt(logFilePath) + "_" + time.Now().Format(time.DateOnly) + "T" + time.Now().Format(time.TimeOnly)
	}

	// Find all log files in the directory of the provided file
	logFiles, err := getLogFilesAbsPaths(logFilePath)
	if err != nil {
		return 0, err
	}

	// Create a temporary directory
	err = os.Mkdir(destName, tmpDirPermissions)

	if err != nil {
		return 0, err
	}
	defer func() {
		_ = os.RemoveAll(destName)
	}() // clean up the temporary dir

	err = copyFilesToDir(destName, logFiles)
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

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, CompressLogsCmd)
	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, CompressLogsCmd)

	CompressLogsCmd.Flags().StringVarP(&compressLogsArgs.destName, "out", "o", "", "output destination file name")
}
