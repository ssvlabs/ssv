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
				OperatorID:      2,
				ValidatorPubKey: "8c5801d7a18e27fae47dfdd99c0ac67fbc6a5a56bb1fc52d0309626d805861e04eaaf67948c18ad50c96d63e44328ab0",
				ValidatorIndex:  fmt.Sprintf("v%d", 1476356),
			}

		case 2:
			corruptedShare = logs_catcher.CorruptedShare{
				OperatorID:      2,
				ValidatorPubKey: "a238aa8e3bd1890ac5def81e1a693a7658da491ac087d92cee870ab4d42998a184957321d70cbd42f9d38982dd9a928c",
				ValidatorIndex:  fmt.Sprintf("v%d", 1476357),
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
