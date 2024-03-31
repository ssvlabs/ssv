package main

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type LogsCatcherCmd struct {
	Mode string `required:"" env:"Mode" help:"Mode of the logs catcher."`
}

func (cmd *LogsCatcherCmd) Run(logger *zap.Logger, globals Globals) error {
	ctx := context.Background()

	cli, err := docker.New()
	if err != nil {
		return fmt.Errorf("failed to open docker client: %w", err)
	}
	defer cli.Close()
	// Initial setup for fatal listener, shared across modes
	if err := cmd.setupFatalListener(ctx, logger, cli); err != nil {
		return err
	}

	// Mode-specific logic
	switch cmd.Mode {
	case logs_catcher.Slashable, logs_catcher.RsaVerification:
		return cmd.processLogs(ctx, logger, cli)
	case logs_catcher.RestartMode:
		return cmd.testRestartNode(ctx, logger, cli)
	case logs_catcher.BlsVerificationMode:
		return cmd.verifyBLSSignatures(ctx, logger, cli, globals.ValidatorsFile)
	default:
		return fmt.Errorf("invalid mode: %s", cmd.Mode)
	}
}

func (cmd *LogsCatcherCmd) setupFatalListener(ctx context.Context, logger *zap.Logger, cli *client.Client) error {
	// Assuming logs_catcher.FatalListener is a common setup function
	err := logs_catcher.FatalListener(ctx, logger, cli)
	if err != nil {
		return fmt.Errorf("failed to set up fatal listener: %w", err)
	}
	return nil
}

func (cmd *LogsCatcherCmd) processLogs(ctx context.Context, logger *zap.Logger, cli *client.Client) error {
	// Example of processing logs based on the mode
	modes := []string{cmd.Mode}
	if cmd.Mode == logs_catcher.Slashable {
		// If Slashable, also check NonSlashable
		modes = append(modes, logs_catcher.NonSlashable)
	}

	for _, mode := range modes {
		matcher := logs_catcher.NewLogMatcher(logger, cli, mode)
		if err := matcher.Match(ctx); err != nil {
			return fmt.Errorf("failed to match logs for mode %s: %w", mode, err)
		}
	}

	return nil
}

func (cmd *LogsCatcherCmd) testRestartNode(ctx context.Context, logger *zap.Logger, cli *client.Client) error {
	// Placeholder for RestartMode logic
	matcher := logs_catcher.NewLogMatcher(logger, cli, "")
	if err := matcher.TestRestartNode(ctx); err != nil {
		return fmt.Errorf("failed to test restart node: %w", err)
	}
	return nil
}

func (cmd *LogsCatcherCmd) verifyBLSSignatures(ctx context.Context, logger *zap.Logger, cli *client.Client, validatorsFile string) error {
	corruptedShares, err := logs_catcher.UnmarshalBlsVerificationJSON(validatorsFile)
	if err != nil {
		return fmt.Errorf("failed to unmarshal bls verification json: %w", err)
	}

	for _, corruptedShare := range corruptedShares {
		if err := logs_catcher.VerifyBLSSignature(ctx, logger, cli, corruptedShare); err != nil {
			return fmt.Errorf("failed to verify BLS signature for validator index %d: %w", corruptedShare.ValidatorIndex, err)
		}
	}
	return nil
}
