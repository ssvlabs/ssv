package logs_catcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/logs"
)

// Test conditions:

const waitTarget = "beacon_proxy"
const firstTarget = "beacon_proxy"

var secondTargets = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}

const waitFor = "End epoch finished"

// For each in target #1
const origMessage = "set up validator"
const slashableMessage = "\"attester_slashable\":true"
const nonSlashableMessage = "\"attester_slashable\":false"

// Take field
const idField = "pubkey"

// and find in target #2
const slashableMatchMessage = "slashable attestation"
const nonSlashableMatchMessage = "successfully submitted attestation"

func StartCondition(pctx context.Context, logger *zap.Logger, condition []string, targetContainer string, cli DockerCLI) (string, error) {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	conditionLog := ""

	logger.Debug("Waiting for start condition at target", zap.String("target", targetContainer), zap.Strings("condition", condition))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			if logs.GrepLine(log, condition) {
				conditionLog = log
				logger.Info("Start condition arrived", zap.Strings("log_message", condition))
				cancel()
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, cli, targetContainer, ch)
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Log streaming stopped with err ", zap.Error(err))
		return conditionLog, err
	}
	return conditionLog, nil
}

// Todo: match messages based on fields. ex: take all X messages from target one,
// 	extract pubkey field and get all matching messages with this pubkey field on target two.

func matchMessages(ctx context.Context, logger *zap.Logger, cli DockerCLI, first []string, second []string, plus int) error {
	res, err := docker.DockerLogs(ctx, cli, firstTarget, "")
	if err != nil {
		return err
	}

	grepped := res.Grep(first)

	logger.Info("matched", zap.Int("count", len(grepped)), zap.String("target", firstTarget), zap.Strings("match_string", first))

	for _, target := range secondTargets {
		logger.Debug("Reading one of second targets logs", zap.String("target", target))

		tres, err := docker.DockerLogs(ctx, cli, target, "")
		if err != nil {
			return err
		}

		tgrepped := tres.Grep(second)

		if len(tgrepped) != len(grepped)+plus {
			return fmt.Errorf("found non matching messages on %v, want %v got %v", target, len(grepped), len(tgrepped))
		}

		logger.Debug("found matching messages for target", zap.Strings("first", first), zap.Strings("second", second), zap.Int("count", len(tgrepped)), zap.String("target", target))
	}

	return nil
}

func Match(pctx context.Context, logger *zap.Logger, cli DockerCLI) error {
	startctx, startc := context.WithTimeout(pctx, time.Minute*6*4) // wait max 4 epochs
	_, err := StartCondition(startctx, logger, []string{waitFor}, waitTarget, cli)
	if err != nil {
		startc() // Cancel the startctx context
		return err
	}
	startc()

	ctx, c := context.WithCancel(pctx)
	defer c()

	// find slashable attestation not signing for each slashable validator
	if err := matchMessages(ctx, logger, cli, []string{origMessage, slashableMessage}, []string{slashableMatchMessage}, 0); err != nil {
		return err
	}

	// find non-slashable validators successfully submitting (all first round + 1 for second round)
	if err := matchMessages(ctx, logger, cli, []string{origMessage, nonSlashableMessage}, []string{nonSlashableMatchMessage}, 30); err != nil {
		return err
	}

	//TODO: match proposals
	return nil
}
