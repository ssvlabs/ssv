package main

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
)

type LogsCatcherCmd struct {
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

	// TODO: should be uncommented and see how we run multiple e2e tests
	//err = logs_catcher.FatalListener(ctx, logger, cli)
	//if err != nil {
	//	return err
	//}

	//err = logs_catcher.Match(ctx, logger, cli)
	//if err != nil {
	//	return err
	//}

	if err = logs_catcher.VerifyBLSSignature(ctx, logger, cli); err != nil {
		return err
	}

	return nil
}
