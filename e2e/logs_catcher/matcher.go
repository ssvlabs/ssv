package logs_catcher

import (
	"context"
	"fmt"
	"time"

	"errors"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/logs"
)

const (
	beaconContainer          = "beacon_proxy"
	endLogCondition          = "End epoch finished"
	messagePrefix            = "set up validator"
	slashableMessage         = "\"attester_slashable\":true"
	nonSlashableMessage      = "\"attester_slashable\":false"
	slashableMatchMessage    = "slashable attestation"
	nonSlashableMatchMessage = "successfully submitted attestation"
)

var secondTargets = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}

func StartCondition(pctx context.Context, logger *zap.Logger, condition []string, targetContainer string, cli DockerCLI) (string, error) {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	var conditionLog string

	logger.Debug("Waiting for start condition at target", zap.String("target", targetContainer), zap.Strings("condition", condition))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			if logs.GrepLine(log, condition) {
				conditionLog = log
				logger.Info("Start condition arrived", zap.Strings("log_message", condition))
				cancel()
				return
			}
		}
	}()

	err := docker.StreamDockerLogs(ctx, cli, targetContainer, ch)
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Log streaming stopped with err", zap.Error(err))
		return conditionLog, err
	}
	return conditionLog, nil
}

func matchMessages(ctx context.Context, logger *zap.Logger, cli DockerCLI, first, second []string, plus int) error {
	res, err := docker.DockerLogs(ctx, cli, beaconContainer, "")
	if err != nil {
		return err
	}

	grepped := res.Grep(first)
	logger.Info("matched", zap.Int("count", len(grepped)), zap.String("target", beaconContainer), zap.Strings("match_string", first))

	for _, target := range secondTargets {
		logger.Debug("Reading logs for second target", zap.String("target", target))

		tres, err := docker.DockerLogs(ctx, cli, target, "")
		if err != nil {
			return err
		}

		tgrepped := tres.Grep(second)
		if len(tgrepped) != len(grepped)+plus {
			return fmt.Errorf("found non matching messages on %v, expected %v, got %v", target, len(grepped), len(tgrepped))
		}

		logger.Debug("Found matching messages for target", zap.Strings("first", first), zap.Strings("second", second), zap.Int("count", len(tgrepped)), zap.String("target", target))
	}

	return nil
}

func Match(pctx context.Context, logger *zap.Logger, cli DockerCLI) error {
	startCtx, startCancel := context.WithTimeout(pctx, 4*time.Minute*6) // wait max 4 epochs
	defer startCancel()

	if _, err := StartCondition(startCtx, logger, []string{endLogCondition}, beaconContainer, cli); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	if err := matchMessages(ctx, logger, cli, []string{messagePrefix, slashableMessage}, []string{slashableMatchMessage}, 0); err != nil {
		return err
	}

	if err := matchMessages(ctx, logger, cli, []string{messagePrefix, nonSlashableMessage}, []string{nonSlashableMatchMessage}, 30); err != nil {
		return err
	}

	// TODO: match proposals
	return nil
}
