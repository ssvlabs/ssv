package main

import (
	"fmt"
	"log"
	"os"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-colorable"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Globals struct {
	NetworkName    string `env:"NETWORK" default:"holesky-e2e" help:"Network config name"`
	LogLevel       string `env:"LOG_LEVEL"       enum:"debug,info,warn,error" default:"debug"             help:"Log level."`
	LogFormat      string `env:"LOG_FORMAT"      enum:"console,json"           default:"console"           help:"Log format."`
	ShareFile      string `env:"SHARE_CONFIG"                              default:"./tconfig/share.yaml" help:"Path to Share file." type:"path"`
	ConfigFile     string `env:"CONFIG_PATH"                              default:"./tconfig/config.yaml" help:"Path to config file." type:"path"`
	ValidatorsFile string `env:"VALIDATORS_FILE"                              default:"./validators.json" help:"Path to the validators.json file." type:"path"`
}

type CLI struct {
	Globals
	BeaconProxy BeaconProxyCmd `cmd:""`
	LogsCatcher LogsCatcherCmd `cmd:""`
	ShareUpdate ShareUpdateCmd `cmd:""`
}

func main() {
	// Parse CLI.
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("ssv-e2e"),
		kong.Description("Toolkit for E2E testing of SSV."),
		kong.UsageOnError(),
		kong.Vars{"version": "0.0.1"},
	)

	// Setup logger.
	logLevel, err := zapcore.ParseLevel(cli.Globals.LogLevel)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to parse log level: %w", err))
	}

	var logger *zap.Logger
	switch cli.Globals.LogFormat {
	case "console":
		encoderConfig := zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		logger = zap.New(zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(colorable.NewColorableStdout()),
			logLevel,
		))
	case "json":
		encoderConfig := zap.NewProductionEncoderConfig()
		logger = zap.New(zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			zapcore.AddSync(os.Stdout),
			logLevel,
		))
	default:
		log.Fatal(fmt.Errorf("unknown log format %q", cli.Globals.LogFormat))
	}

	// Run the CLI.
	err = ctx.Run(logger, cli.Globals)

	if err != nil {
		logger.Fatal("run stopped", zap.Error(err))
	}
}
