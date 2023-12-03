package main

import (
	"fmt"
	"log"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-colorable"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(colorable.NewColorableStdout()),
		logLevel,
	))

	// Run the CLI.
	err = ctx.Run(logger, cli.Globals)
	ctx.FatalIfErrorf(err)
}
