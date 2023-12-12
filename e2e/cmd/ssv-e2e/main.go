package main

import (
	"fmt"
	"github.com/alecthomas/kong"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
)

type Globals struct {
	LogLevel       string `env:"LOG_LEVEL"       enum:"debug,info,warn,error" default:"debug"             help:"Log level."`
	ValidatorsFile string `env:"VALIDATORS_FILE"                              default:"./validators.json" help:"Path to the validators.json file." type:"path"`
}

type CLI struct {
	Globals
	BeaconProxy BeaconProxyCmd `cmd:""`
	LogsCatcher LogsCatcherCmd `cmd:""`
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

	//encoderConfig := zap.NewDevelopmentEncoderConfig()
	//encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	//logger := zap.New(zapcore.NewCore(
	//	zapcore.NewConsoleEncoder(encoderConfig),
	//	zapcore.AddSync(colorable.NewColorableStdout()),
	//	logLevel,
	//))

	// uncomment to replace to json logs - needed for logs_catcher
	encoderConfig := zap.NewProductionEncoderConfig()
	//	encoderConfig.EncodeLevel = zapcore.json
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		logLevel,
	))

	// Run the CLI.
	err = ctx.Run(logger, cli.Globals)

	if err != nil {
		logger.Fatal("run stopped", zap.Error(err))
	}
}
