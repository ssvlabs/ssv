package logs_catcher

import (
	"context"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/logs"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
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

func StartCondition(pctx context.Context, logger *zap.Logger, matches []string, cli DockerCLI) error {
	ctx, c := context.WithCancel(pctx)

	logger.Debug("Waiting for start condition at target", zap.String("target", waitTarget), zap.String("condition", waitFor))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			if logs.GrepLine(log, matches) {
				logger.Info("Start condition arrived", zap.String("log_message", waitFor))
				c()
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, cli, waitTarget, ch)
	if !errors.Is(err, context.Canceled) {
		logger.Error("Log streaming stopped with err ", zap.Error(err))
		c()
		return err
	}
	return nil
}

func matchMessages(ctx context.Context, logger *zap.Logger, cli DockerCLI, first []string, second []string) error {
	res, err := docker.DockerLogs(ctx, cli, firstTarget, "")
	if err != nil {
		return err
	}

	grepped := res.Grep(first)

	// TODO: verify pubkeys
	//parsed := res.ParseAll(parser.JSON)
	//
	//find := make(map[string]func(any) bool, 0)
	//find[MesasgeKey] = func(a any) bool {
	//	if str, ok := a.(string); ok {
	//		if str == origMessage {
	//			return true
	//		}
	//	}
	//	return false
	//}
	//find["attester_slashable"] = func(a any) bool {
	//	if str, ok := a.(bool); ok {
	//		if str {
	//			return true
	//		}
	//	}
	//	return false
	//}

	//grepped := parsed.GrepCondition(find)

	logger.Info("found instances", zap.Int("count", len(grepped)), zap.String("target", firstTarget), zap.String("match_string", origMessage))

	for _, target := range secondTargets {
		logger.Debug("Reading one of second targets logs", zap.String("target", target))

		tres, err := docker.DockerLogs(ctx, cli, target, "")
		if err != nil {
			return err
		}

		//TODO: field based
		//tparsed := tres.ParseAll(parser.JSON)
		//
		//tfind := make(map[string]func(any) bool, 0)
		//tfind["error"] = func(a any) bool {
		//	if str, ok := a.(string); ok {
		//		return strings.Contains(str, slashableMatchMessage)
		//	}
		//	return false
		//}
		//
		//tgrepped := tparsed.GrepCondition(tfind)

		tgrepped := tres.Grep(second)

		if len(tgrepped) != len(grepped) {
			return errors.Errorf("found non matching messages on %v, want %v got %v", target, len(grepped), len(tgrepped))
		}

		logger.Debug("found matching messages for target", zap.Strings("first", first), zap.Strings("second", second), zap.Int("count", len(tgrepped)), zap.String("target", target))
	}

	return nil
}

func Match(pctx context.Context, logger *zap.Logger, cli DockerCLI) error {
	startctx, startc := context.WithTimeout(pctx, time.Minute*6*4) // wait max 4 epochs
	err := StartCondition(startctx, logger, []string{waitFor}, cli)
	if err != nil {
		return err
	}
	startc()

	ctx, c := context.WithCancel(pctx)
	defer c()

	// Find slashable
	if err := matchMessages(ctx, logger, cli, []string{origMessage, slashableMessage}, []string{slashableMatchMessage}); err != nil {
		return err
	}

	// find non-slashable
	if err := matchMessages(ctx, logger, cli, []string{origMessage, nonSlashableMessage}, []string{nonSlashableMatchMessage}); err != nil {
		return err
	}

	//TODO: match proposals
	return nil
}
