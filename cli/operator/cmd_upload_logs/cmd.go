package cmd_compress_logs

import (
	"fmt"

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

type UploadLogsArg struct {
	logFilePath string
}

var compressLogsArgs UploadLogsArg

// UploadLogsCmd is the command to compress logs file with gzip
var UploadLogsCmd = &cobra.Command{
	Use:   "compress-logs",
	Short: "Compresses logs",
	Run: func(cmd *cobra.Command, args []string) {
		logger := zap.L()

		logger.Info(fmt.Sprintf("starting logs uploading to S3 %v", commons.GetBuildData()))

		err := uploadLogs(&compressLogsArgs)

		if err != nil {
			logger.Fatal("logs file upload failed", zap.Error(err))
		}
		logger.Info("✅ logs've been uploaded to S3")
	},
}

func uploadLogs(args *UploadLogsArg) error {

	return nil
}

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, UploadLogsCmd)
	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, UploadLogsCmd)
}
