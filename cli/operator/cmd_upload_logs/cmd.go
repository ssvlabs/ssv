package cmd_compress_logs

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

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
	tarFilePath string
	url         string
}

var compressLogsArgs UploadLogsArg

// UploadLogsCmd is the command to compress logs file with gzip
var UploadLogsCmd = &cobra.Command{
	Use:   "compress-logs",
	Short: "Compresses logs",
	Run: func(cmd *cobra.Command, args []string) {
		logger := zap.L()

		logger.Info(fmt.Sprintf("starting logs uploading to S3 %v", commons.GetBuildData()))

		err := uploadLogs(compressLogsArgs.tarFilePath, compressLogsArgs.url)

		if err != nil {
			logger.Fatal("logs file upload failed", zap.Error(err))
		}
		logger.Info("✅ logs have been uploaded to S3")
	},
}

func uploadLogs(tarPath string, url string) error {
	return nil
}

func UploadFile(filePath string, url string) error {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	buffer := bytes.NewBuffer(nil)
	if _, err := io.Copy(buffer, file); err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPut, url, buffer)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "multipart/form-data")
	client := &http.Client{}
	_, err = client.Do(request)

	return err
}

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, UploadLogsCmd)
	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, UploadLogsCmd)
}
