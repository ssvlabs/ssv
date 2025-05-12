package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
)

type LogsCatcherCmd struct {
	Mode string `required:"" env:"Mode" help:"Mode of the logs catcher. Can be Slashing or BlsVerification"`
}

const (
	SlashingMode        = "Slashing"
	BlsVerificationMode = "BlsVerification"
)

type BlsVerificationJSON struct {
	CorruptedShares []*logs_catcher.CorruptedShare `json:"bls_verification"`
}

func (cmd *LogsCatcherCmd) Run(logger *zap.Logger, globals Globals) error {
	// TODO: where do we stop?
	ctx := context.Background()

	cli, err := docker.New()
	if err != nil {
		return fmt.Errorf("failed to open docker client: %w", err)
	}
	defer cli.Close()

	//TODO: run fataler and matcher in parallel?

	// Execute different logic based on the value of the Mode flag
	switch cmd.Mode {
	case SlashingMode:
		logger.Info("Running slashing mode")
		err = logs_catcher.FatalListener(ctx, logger, cli)
		if err != nil {
			return err
		}
		err = logs_catcher.Match(ctx, logger, cli)
		if err != nil {
			return err
		}

	case BlsVerificationMode:
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
	contents, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("error reading json file for BLS verification: %s, %w", filePath, err)
	}

	var blsVerificationJSON BlsVerificationJSON
	if err = json.Unmarshal(contents, &blsVerificationJSON); err != nil {
		return nil, fmt.Errorf("error parsing json file for BLS verification: %s, %w", filePath, err)
	}

	return blsVerificationJSON.CorruptedShares, nil
}
