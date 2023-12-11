package logs_catcher

import (
	"context"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/parser"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
	"time"
)

// Test conditions: // TODO: parameterise?

const waitTarget = "beacon_proxy"
const firstTarget = "beacon_proxy"

var secondTargets = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}

const waitFor = "End epoch finished"

// For each in target #1
const origMessage = "set up validator"

// Take field
const idField = "pubkey"

// and find in target #2
const matchMessage = "slashable attestation"

func StartCondition(pctx context.Context, logger *zap.Logger, cfg Config, cli DockerCLI) error {
	ctx, c := context.WithCancel(pctx)

	logger.Debug("Waiting for start condition at target", zap.String("target", waitTarget), zap.String("condition", waitFor))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			p, err := parser.JSON(log)
			if err != nil {
				logger.Warn("failed to parse log", zap.Error(err), zap.String("log", log))
				continue
			}
			if p[MesasgeKey] == waitFor {
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
		close(ch) // not writing since StreamDockerLogs is done
		return err
	}
	return nil
}

func Match(pctx context.Context, logger *zap.Logger, cfg Config, cli DockerCLI) error {
	startctx, startc := context.WithTimeout(pctx, time.Minute*6*4) // wait max 4 epochs
	err := StartCondition(startctx, logger, cfg, cli)
	startc()
	if err != nil {
		return err
	}

	ctx, c := context.WithCancel(pctx)
	defer c()

	logger.Debug("Reading first target logs", zap.String("target", firstTarget))

	res, err := docker.DockerLogs(ctx, cli, firstTarget)
	if err != nil {

		return err
	}

	parsed := res.ParseAll(parser.JSON)

	find := make(map[string]func(any) bool, 0)
	find[MesasgeKey] = func(a any) bool {
		if str, ok := a.(string); ok {
			if str == origMessage {
				return true
			}
		}
		return false
	}
	find["attester_slashable"] = func(a any) bool {
		if str, ok := a.(bool); ok {
			if str {
				return true
			}
		}
		return false
	}

	grepped := parsed.GrepCondition(find)

	logger.Info("found instances", zap.Int("count", len(grepped)), zap.String("target", firstTarget), zap.String("match_string", origMessage))

	for _, target := range secondTargets {
		logger.Debug("Reading one of second targets logs", zap.String("target", target))

		tres, err := docker.DockerLogs(ctx, cli, target)
		if err != nil {
			return err
		}

		tparsed := tres.ParseAll(parser.JSON)

		tfind := make(map[string]func(any) bool, 0)
		find[MesasgeKey] = func(a any) bool {
			if str, ok := a.(string); ok {
				if strings.Contains(str, matchMessage) {
					return true
				}
			}
			return false
		}

		tgrepped := tparsed.GrepCondition(tfind)

		if len(tgrepped) != len(grepped) {
			return errors.Errorf("couldn't find enough of match messages on %v, want %v got %v", target, len(grepped), len(tgrepped))
		}

		logger.Debug("found slashable attestation log for each slashable validator ", zap.String("target", target))
	}
	return nil
}
