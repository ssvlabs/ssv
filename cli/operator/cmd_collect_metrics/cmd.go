package cmd_compress_logs

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	globalconfig "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/utils/commons"
)

type config struct {
	globalconfig.GlobalConfig `yaml:"global"`
}

type CollectMetricsArgs struct {
	promRoute string
}

var cfg config
var globalArgs globalconfig.Args
var collectMetricsArgs CollectMetricsArgs

var CollectMetricsCmd = &cobra.Command{
	Use:   "compress-logs",
	Short: "Compresses logs",
	Run: func(cmd *cobra.Command, args []string) {
		logger := zap.L()

		logger.Info(fmt.Sprintf("starting logs uploading to S3 %v", commons.GetBuildData()))

		collectMetricsArgs = CollectMetricsArgs{
			promRoute: "localhost:9090/metrics",
		}

		err := collectMetrics(&collectMetricsArgs)

		if err != nil {
			logger.Fatal("logs file upload failed", zap.Error(err))
		}
		logger.Info("✅ logs've been uploaded to S3")
	},
}

func collectMetrics(args *CollectMetricsArgs) error {
	resp, err := http.Get("http://" + args.promRoute)
	if err != nil {
		return fmt.Errorf("failed to get metrics from route %s with error: %w", args.promRoute, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	file, err := os.Create("metrics_dump.txt")
	if err != nil {
		return fmt.Errorf("failed to create dump file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	return nil
}

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, CollectMetricsCmd)
	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, CollectMetricsCmd)
}
