package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
)

type LogsCatcherCmd struct {
	Mode string `required:"" env:"Mode" help:"Mode of the logs catcher."`
}

type BlsVerificationJSON struct {
	CorruptedShares []*logs_catcher.CorruptedShare `json:"bls_verification"`
}

// InterpretMode takes the input string and maps it to the appropriate ModeType.
func (cmd *LogsCatcherCmd) InterpretMode() logs_catcher.Mode {
	switch cmd.Mode {
	case logs_catcher.Slashable, logs_catcher.RsaVerification:
		return logs_catcher.DutiesMode
	case logs_catcher.BlsVerificationMode:
		return logs_catcher.BlsVerificationMode
	default:
		return "" // Indicates an unsupported mode
	}
}

func (cmd *LogsCatcherCmd) Run(logger *zap.Logger, globals Globals) error {
	// TODO: where do we stop?
	ctx := context.Background()

	cli, err := docker.New()
	if err != nil {
		return fmt.Errorf("failed to open docker client: %w", err)
	}
	defer cli.Close()

	// Convert the input string to a ModeType using InterpretMode method
	mode := cmd.InterpretMode()

	//TODO: run fataler and matcher in parallel?

	// Execute different logic based on the value of the Mode flag
	switch mode {
	case logs_catcher.DutiesMode:
		logger.Info("Running", zap.String("mode: ", cmd.Mode))
		err = logs_catcher.FatalListener(ctx, logger, cli)
		if err != nil {
			return err
		}

		matcher := logs_catcher.NewLogMatcher(logger, cli, cmd.Mode)
		err = matcher.Match(ctx)
		if err != nil {
			return err
		}
	case logs_catcher.BlsVerificationMode:
		logger.Info("Running BlsVerification mode")
		corruptedShares, err := UnmarshalBlsVerificationJSON(globals.ValidatorsFile)
		if err != nil {
			return fmt.Errorf("failed to unmarshal bls verification json: %w", err)
		}

		for _, corruptedShare := range corruptedShares {
			if err = logs_catcher.VerifyBLSSignature(ctx, logger, cli, corruptedShare); err != nil {
				return fmt.Errorf("failed to verify BLS signature for validator index %d: %w", corruptedShare.ValidatorIndex, err)
			}
		}
	default:
		return fmt.Errorf("invalid mode: %s", cmd.Mode)
	}

	return nil
}

// UnmarshalBlsVerificationJSON reads the JSON file and unmarshals it into []*CorruptedShare.
func UnmarshalBlsVerificationJSON(filePath string) ([]*logs_catcher.CorruptedShare, error) {
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error reading json file for BLS verification: %s, %w", filePath, err)
	}

	var blsVerificationJSON BlsVerificationJSON
	if err = json.Unmarshal(contents, &blsVerificationJSON); err != nil {
		return nil, fmt.Errorf("error parsing json file for BLS verification: %s, %w", filePath, err)
	}

	return blsVerificationJSON.CorruptedShares, nil
}
