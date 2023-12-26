package main

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
)

type LogsCatcherCmd struct {
	Mode   string `required:"" env:"Mode" help:"Mode of the logs catcher. Can be Slashing or BlsVerification"`
	Leader int    `env:"Leader" help:"Leader to run the bls verification on"`
}

const (
	SlashingMode        = "Slashing"
	BlsVerificationMode = "BlsVerification"
)

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

		var corruptedShare logs_catcher.CorruptedShare
		switch cmd.Leader {
		case 1:
			corruptedShare = logs_catcher.CorruptedShare{
				OperatorID:      4,
				ValidatorPubKey: "8c5801d7a18e27fae47dfdd99c0ac67fbc6a5a56bb1fc52d0309626d805861e04eaaf67948c18ad50c96d63e44328ab0",
				ValidatorIndex:  fmt.Sprintf("v%d", 1476356),
			}

		case 4:
			corruptedShare = logs_catcher.CorruptedShare{
				OperatorID:      4,
				ValidatorPubKey: "81bde622abeb6fb98be8e6d281944b11867c6ddb23b2af582b2af459a0316f766fdb97e56a6c69f66d85e411361c0b8a",
				ValidatorIndex:  fmt.Sprintf("v%d", 1476359),
			}
		default:
			return fmt.Errorf("invalid leader: %d", cmd.Leader)
		}

		if err = logs_catcher.VerifyBLSSignature(ctx, logger, cli, corruptedShare); err != nil {
			return err
		}

	default:
		return fmt.Errorf("invalid mode: %s", cmd.Mode)
	}

	return nil
}
