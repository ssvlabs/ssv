package cmd_compress_logs

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	globalconfig "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type config struct {
	globalconfig.GlobalConfig `yaml:"global"`
}

var cfg config
var globalArgs globalconfig.Args

// CompressLogsCmd is the command to compress logs file with gzip
var CompressLogsCmd = &cobra.Command{
	Use:   "compress-logs",
	Short: "Compresses logs",
	Run: func(cmd *cobra.Command, args []string) {
		logger, err := setupGlobal(&cfg)

		if err != nil {
			logger.Fatal("initialization failed", zap.Error(err))
		}

		logger.Info(fmt.Sprintf("starting logs compression%v", commons.GetBuildData()))

		logsFilePath := cfg.LogFilePath

		fileSizeBytes, err := compressFile(logsFilePath)
		if err != nil {
			logger.Fatal("logs file compression failed", zap.Error(err))
		}

		logger.Info("✅ logs compression finished", zap.Uint64("file_size_bytes", uint64(fileSizeBytes)))
	},
}

func compressFile(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	compressedFile, err := os.Create(path + ".gz")
	if err != nil {
		return 0, err
	}
	defer compressedFile.Close()

	gzWriter := gzip.NewWriter(compressedFile)
	if _, err := io.Copy(gzWriter, file); err != nil {
		return 0, err
	}

	if err := gzWriter.Close(); err != nil {
		return 0, err
	}

	// get the size of the compressed file
	info, err := compressedFile.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func setupGlobal(cfg *config) (*zap.Logger, error) {
	if globalArgs.ConfigPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	if err := cleanenv.ReadConfig(globalArgs.ConfigPath, cfg); err != nil {
		return nil, fmt.Errorf("could not read config: %w", err)
	}

	return setGlobalLogger(cfg)
}

func setGlobalLogger(cfg *config) (*zap.Logger, error) {
	err := logging.SetGlobalLogger(
		cfg.LogLevel,
		cfg.LogLevelFormat,
		"console",
		nil,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to set global logger: %w", err)
	}

	return zap.L(), nil
}

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, CompressLogsCmd)
	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, CompressLogsCmd)
}
